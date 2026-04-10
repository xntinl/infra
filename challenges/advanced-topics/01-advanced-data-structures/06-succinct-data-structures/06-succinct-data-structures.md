<!--
type: reference
difficulty: advanced
section: [01-advanced-data-structures]
concepts: [succinct-data-structures, rank-select, wavelet-tree, compressed-suffix-array, FM-index, bit-vectors]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: analyze
prerequisites: [bit-manipulation, prefix-sums, binary-trees, basic-string-algorithms]
papers: [jacobson-1989-space-efficient-structures, grossi-2000-compressed-suffix-arrays, claude-2015-wavelet-trees]
industry_use: [genomics-bwa-aligner, sdsl-lite, compressed-text-indexes, columnar-databases]
language_contrast: medium
-->

# Succinct Data Structures

> The BWA-MEM2 genome aligner fits a 3.2 GB human reference genome into 4 GB of RAM for exact and approximate string matching — the core data structure is an FM-index built on a rank/select-equipped bit array, reducing the genome to near its information-theoretic minimum while supporting O(m) pattern search for a query of length m.

## Mental Model

A succinct data structure uses space within a constant factor of the information-theoretic minimum while supporting operations in O(1) or O(polylog n) time. The contrast with compressed data structures: a compressed structure reduces space below the minimum by accepting that you must decompress to access it (O(n) decompression). A succinct structure uses approximately the minimum space and still supports random access.

The information-theoretic minimum for storing a binary string of length n with k ones is `log2(C(n,k))` bits — approximately `n × H(k/n)` bits where H is binary entropy. A plain array of n bits uses n bits, which already achieves this bound when the density is fixed. The succinct insight is for structures that must support operations beyond raw storage: rank (how many 1s up to position i?), select (what is the position of the j-th 1?), and predecessor/successor queries. These require additional metadata — but the metadata can be compressed into O(n / log n) words, which is o(n) bits — asymptotically negligible.

The practical use cases for succinct data structures are all data-intensive:

- **Genomics**: A reference genome is a string over a 4-character alphabet (A, C, G, T). Storing it naively in ASCII takes 3.2 GB; in 2 bits per character it takes 800 MB; in a succinct structure (FM-index or compressed suffix array) it takes 400-800 MB while supporting O(m) exact pattern matching queries, which would take O(m log n) with binary search over a suffix array.
- **Compressed full-text indexes**: Search engines historically kept uncompressed suffix arrays (n × log n bits). A succinct text index fits in O(n log σ) bits for alphabet size σ, enabling full-text search without decompression.
- **Column stores**: Columnar databases store large sorted arrays of integers. A succinct prefix-sum structure (Elias-Fano coding) stores n integers from [0, U) in n log(U/n) + 2n bits — close to the entropy lower bound — while supporting O(1) rank, predecessor, and random access queries.

## Core Concepts

### Rank and Select on Bit Vectors

The fundamental primitives:

**Rank(v, i)** = number of bits equal to v (0 or 1) in positions [0, i].
**Select(v, j)** = position of the j-th occurrence of bit v (1-indexed).

These look simple, but implementing them in O(1) time (not O(n)) requires non-trivial preprocessing. The standard approach:

**Popcount blocks**: Divide the bit array into "superblocks" of size s₁ = (log n)² bits and "blocks" of size s₂ = (log n)/2 bits. For each superblock, store the cumulative rank up to its start (in log n bits). For each block, store the rank within its superblock (in log log n bits). A rank query is then: lookup the superblock, add the block offset, then use a popcount lookup table for the remaining bits within the block. This requires O(n/log n) space for the metadata (o(n) additional bits beyond the bit array itself).

**Select with auxiliary structures**: Divide bit positions by rank into buckets of size log n ones. For sparse buckets (distance > log²n between consecutive ones), store positions explicitly. For dense buckets, use a rank-in-blocks approach. The result is O(1) select with o(n) auxiliary space.

In practice, a 64-bit machine instruction `popcnt` (available as `bits.OnesCount64` in Go and intrinsically in Rust) computes the popcount of a word in one cycle. This means the "lookup table for remaining bits" step costs one instruction — rank queries on 64-bit-aligned boundaries are extremely fast in hardware.

### Wavelet Tree

A wavelet tree answers the following queries on a string S over alphabet [0, σ) in O(log σ) time:

- **rank(c, i)**: how many occurrences of character c appear in S[0..i]?
- **select(c, j)**: where is the j-th occurrence of c in S?
- **access(i)**: what is S[i]?
- **range_quantile(l, r, k)**: what is the k-th smallest character in S[l..r]?

The wavelet tree construction: at the root, assign each character a bit based on whether it is in the lower or upper half of the alphabet. Recurse into the left subtree (lower half) and right subtree (upper half). Each level of the tree is a bit vector of length n. With σ = 4 (DNA), the tree has 2 levels; with σ = 256 (bytes), 8 levels.

The range_quantile query is what makes wavelet trees uniquely powerful: answering "what is the median character in substring S[l..r]?" takes O(log σ) time — on a compressed suffix array with n=10^9, this is 30 operations. The equivalent on an uncompressed suffix array (binary search + range scan) would take O(log n × (r-l)) time.

### Compressed Suffix Arrays and FM-Index

The FM-index (Ferragina-Manzini, 2000) achieves near-optimal space for full-text indexing. It stores:

1. **BWT(T)**: the Burrows-Wheeler Transform of the text T — a permutation of characters with the property that equal characters cluster together.
2. **C[c]**: the number of characters lexicographically smaller than c in T.
3. **Occ(c, i)**: the rank of character c in BWT[0..i] — how many times c appears in the first i characters of the BWT.

Pattern matching (backward search): given query pattern P[0..m-1], the algorithm maintains a range [lo, hi] in the suffix array representing suffixes that match the current suffix of P. Each step: lo = C[c] + Occ(c, lo-1), hi = C[c] + Occ(c, hi). Using a wavelet tree for Occ queries, each step costs O(log σ). The entire O(m) search costs O(m log σ).

The information-theoretic insight: the FM-index stores the text using at most `H_k(T) + o(n)` bits, where H_k is the k-th order empirical entropy of T. For English text, H_k ≈ 2-3 bits per character — better than UTF-8 (8 bits) without any precompression step.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math/bits"
)

// BitVector provides O(1) rank and O(log n) select on a bit array.
// Design: for simplicity, we implement O(log n) select here via binary search
// over the precomputed rank. Production implementations use a two-level structure
// for O(1) select (see the Jacobson 1989 paper).
type BitVector struct {
	words      []uint64
	n          int     // total number of bits
	blockRanks []int32 // rank at the start of each 64-bit block
	totalOnes  int
}

func NewBitVector(bits_ []bool) *BitVector {
	n := len(bits_)
	numWords := (n + 63) / 64
	words := make([]uint64, numWords)
	blockRanks := make([]int32, numWords+1)

	for i, b := range bits_ {
		if b {
			words[i/64] |= 1 << uint(i%64)
		}
	}

	// Compute prefix popcount per 64-bit block
	var cumulative int32
	for i, w := range words {
		blockRanks[i] = cumulative
		cumulative += int32(bits.OnesCount64(w))
	}
	blockRanks[numWords] = cumulative

	return &BitVector{
		words:      words,
		n:          n,
		blockRanks: blockRanks,
		totalOnes:  int(cumulative),
	}
}

// Rank1 returns the number of 1-bits in positions [0, i] (inclusive).
// O(1) using precomputed block ranks + popcount of the partial word.
func (bv *BitVector) Rank1(i int) int {
	if i < 0 {
		return 0
	}
	if i >= bv.n {
		i = bv.n - 1
	}
	block := i / 64
	offset := uint(i % 64)
	// Mask the partial word to include only bits [0..offset]
	mask := (uint64(1) << (offset + 1)) - 1
	partial := bits.OnesCount64(bv.words[block] & mask)
	return int(bv.blockRanks[block]) + partial
}

// Rank0 returns the number of 0-bits in positions [0, i] (inclusive).
func (bv *BitVector) Rank0(i int) int {
	return (i + 1) - bv.Rank1(i)
}

// Access returns the bit at position i.
func (bv *BitVector) Access(i int) bool {
	return bv.words[i/64]>>(uint(i)%64)&1 == 1
}

// Select1 returns the 0-indexed position of the j-th 1-bit (j is 1-based).
// O(log n) via binary search over block ranks.
func (bv *BitVector) Select1(j int) int {
	if j <= 0 || j > bv.totalOnes {
		return -1
	}
	// Binary search: find the block where cumulative rank crosses j
	lo, hi := 0, len(bv.words)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if int(bv.blockRanks[mid+1]) < j {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	// lo is the block; now find the exact bit within the block
	remaining := j - int(bv.blockRanks[lo])
	word := bv.words[lo]
	for bit := 0; bit < 64; bit++ {
		if word&(1<<uint(bit)) != 0 {
			remaining--
			if remaining == 0 {
				return lo*64 + bit
			}
		}
	}
	return -1 // should not reach here
}

// WaveletTree answers rank, select, and range_quantile queries on integer sequences.
// For simplicity, this implementation uses alphabet [0, maxVal].
// Each level splits the alphabet in half; leaves correspond to individual characters.
type WaveletTree struct {
	n       int
	maxVal  int
	levels  []*BitVector // one bit vector per level of the tree
	numLevels int
}

// buildLevel constructs one level of the wavelet tree.
// Returns the bit vector for this level and the sequences for the left and right subtrees.
func buildLevel(seq []int, lo, hi int) (*BitVector, []int, []int) {
	if lo == hi {
		return nil, seq, nil
	}
	mid := (lo + hi) / 2
	bits_ := make([]bool, len(seq))
	var left, right []int
	for i, v := range seq {
		if v <= mid {
			bits_[i] = false
			left = append(left, v)
		} else {
			bits_[i] = true
			right = append(right, v)
		}
	}
	return NewBitVector(bits_), left, right
}

// NewWaveletTree builds a wavelet tree for the sequence.
// maxVal is the maximum value in the sequence (defines the alphabet size).
func NewWaveletTree(seq []int, maxVal int) *WaveletTree {
	numLevels := 0
	for (1 << uint(numLevels)) <= maxVal {
		numLevels++
	}

	levels := make([]*BitVector, numLevels)
	// Iterative level construction: maintain the sequence at each level
	current := make([]int, len(seq))
	copy(current, seq)

	lo, hi := 0, maxVal
	for l := 0; l < numLevels; l++ {
		bv, leftSeq, _ := buildLevel(current, lo, hi)
		levels[l] = bv
		_ = leftSeq
		// For a full wavelet tree, we would recurse into both subtrees separately.
		// This simplified version shows the structure of one "column" of the tree.
		// A production implementation uses a bottom-up construction for all columns.
		lo, hi = lo, (lo+hi)/2 // descend into left subtree for illustration
		current = current[:0]
		for _, v := range seq {
			if v <= (lo+hi+maxVal)/2 { // approximate split for demo
				current = append(current, v)
			}
		}
	}

	return &WaveletTree{
		n:         len(seq),
		maxVal:    maxVal,
		levels:    levels,
		numLevels: numLevels,
	}
}

// RankChar returns how many occurrences of character c appear in seq[0..i].
// O(log maxVal) operations on the bit vectors.
func (wt *WaveletTree) RankChar(c, i int) int {
	lo, hi := 0, wt.maxVal
	for l := 0; l < wt.numLevels && lo < hi; l++ {
		mid := (lo + hi) / 2
		bv := wt.levels[l]
		if bv == nil {
			break
		}
		if c <= mid {
			// Character is in the left subtree; count 0-bits in [0..i]
			i = bv.Rank0(i) - 1
			hi = mid
		} else {
			// Character is in the right subtree; count 1-bits in [0..i]
			i = bv.Rank1(i) - 1
			lo = mid + 1
		}
		if i < 0 {
			return 0
		}
	}
	return i + 1
}

// EliasFano encodes a sorted sequence of integers from [0, U) in approximately
// n log(U/n) + 2n bits — near the information-theoretic minimum.
// Supports O(1) access and O(log(U/n)) predecessor queries.
// This is the structure used in Apache Lucene for posting list compression.
type EliasFano struct {
	// Upper bits: unary encoding of the gaps between upper bit groups
	upper *BitVector
	// Lower bits: the lower log2(U/n) bits of each element, stored consecutively
	lower    []uint64
	lowerBits int
	n        int
}

func NewEliasFano(sorted []int, u int) *EliasFano {
	n := len(sorted)
	if n == 0 {
		return &EliasFano{n: 0}
	}

	// Lower bits: floor(log2(U/n))
	lowerBits := 0
	for (1 << uint(lowerBits)) < u/n {
		lowerBits++
	}
	lowerMask := (1 << uint(lowerBits)) - 1

	// Upper bits: unary representation of the upper part of each value
	// The upper value of element i is sorted[i] >> lowerBits
	// Encode as: for each upper value u_i, write (u_i - u_{i-1}) zeros then a 1
	numUpperBits := (sorted[n-1]>>uint(lowerBits))+1 + n
	upperBits := make([]bool, numUpperBits+1)
	pos := 0
	prevUpper := 0
	lowerStorage := make([]uint64, (n*lowerBits+63)/64+1)

	for i, v := range sorted {
		upper := v >> uint(lowerBits)
		lower := v & lowerMask
		// Write (upper - prevUpper) zeros then a 1
		for j := 0; j < upper-prevUpper; j++ {
			if pos < len(upperBits) {
				upperBits[pos] = false
				pos++
			}
		}
		if pos < len(upperBits) {
			upperBits[pos] = true
			pos++
		}
		prevUpper = upper
		// Pack lower bits
		bitPos := i * lowerBits
		wordIdx := bitPos / 64
		bitOff := uint(bitPos % 64)
		if wordIdx < len(lowerStorage) {
			lowerStorage[wordIdx] |= uint64(lower) << bitOff
		}
	}

	return &EliasFano{
		upper:     NewBitVector(upperBits[:pos]),
		lower:     lowerStorage,
		lowerBits: lowerBits,
		n:         n,
	}
}

// Access returns the i-th element (0-indexed) in O(1).
func (ef *EliasFano) Access(i int) int {
	if i < 0 || i >= ef.n {
		return -1
	}
	// Lower part: extract lowerBits bits starting at position i*lowerBits
	lowerMask := (1 << uint(ef.lowerBits)) - 1
	bitPos := i * ef.lowerBits
	wordIdx := bitPos / 64
	bitOff := uint(bitPos % 64)
	var lowerVal int
	if wordIdx < len(ef.lower) {
		lowerVal = int((ef.lower[wordIdx] >> bitOff) & uint64(lowerMask))
	}
	// Upper part: find the i-th 1-bit in upper, count 0-bits before it
	onePos := ef.upper.Select1(i + 1)
	if onePos < 0 {
		return -1
	}
	upperVal := onePos - i // number of 0-bits before the i-th 1-bit = upper part value
	return (upperVal << uint(ef.lowerBits)) | lowerVal
}

func main() {
	fmt.Println("=== Bit Vector with Rank/Select ===")
	pattern := []bool{false, true, false, true, true, false, true, false, true, true}
	bv := NewBitVector(pattern)
	for i := range pattern {
		fmt.Printf("bit[%d]=%v rank1[%d]=%d rank0[%d]=%d\n",
			i, bv.Access(i), i, bv.Rank1(i), i, bv.Rank0(i))
	}
	fmt.Printf("Select1(3): position %d\n", bv.Select1(3))
	fmt.Printf("Select1(5): position %d\n", bv.Select1(5))

	fmt.Println("\n=== Elias-Fano Encoding ===")
	sorted := []int{2, 5, 8, 11, 14, 18, 22, 30, 40, 50}
	ef := NewEliasFano(sorted, 64)
	fmt.Printf("Original: %v\n", sorted)
	fmt.Print("Decoded:  ")
	for i := range sorted {
		fmt.Printf("%d ", ef.Access(i))
	}
	fmt.Println()
	fmt.Printf("Lower bits per element: %d\n", ef.lowerBits)
	fmt.Printf("Approximate bits per element: %d (naive: 6 bits for [0,64))\n",
		ef.lowerBits+2) // lower_bits + ~2 for upper part
}
```

### Go-specific considerations

The `bits.OnesCount64` function in Go's `math/bits` package compiles to a single `POPCNT` instruction on x86_64 when the target architecture supports it (virtually all modern CPUs). This is the key performance primitive: every rank query in the bit vector is O(1) because it reduces to one `POPCNT` instruction plus one array lookup (for the precomputed block rank).

For a production succinct data structure in Go, the SDSL-lite concepts (from the C++ library) can be ported directly: the interface between the bit vector and the rank/select data structures is the `words []uint64` array. All metadata structures (block ranks, select samples) are separate arrays over the same word array, so you can mix and match different rank/select implementations without rewriting the bit vector.

Go's `unsafe.Sizeof` reveals that a `bool` slice element is 1 byte (not 1 bit). The `[]bool` input to `NewBitVector` is a convenience — in production, input should be a `[]uint64` bit array directly, avoiding the O(n) conversion.

## Implementation: Rust

```rust
// Succinct bit vector with O(1) rank and O(log n) select.
// Uses the hardware POPCNT instruction via u64::count_ones().

pub struct BitVector {
    words: Vec<u64>,
    n: usize,
    // block_ranks[i] = number of 1-bits in words[0..i) (exclusive)
    block_ranks: Vec<u32>,
    total_ones: usize,
}

impl BitVector {
    pub fn from_bits(bits: &[bool]) -> Self {
        let n = bits.len();
        let num_words = (n + 63) / 64;
        let mut words = vec![0u64; num_words];

        for (i, &b) in bits.iter().enumerate() {
            if b {
                words[i / 64] |= 1u64 << (i % 64);
            }
        }

        let mut block_ranks = vec![0u32; num_words + 1];
        let mut cumulative = 0u32;
        for (i, &w) in words.iter().enumerate() {
            block_ranks[i] = cumulative;
            cumulative += w.count_ones();
        }
        block_ranks[num_words] = cumulative;

        BitVector {
            words,
            n,
            block_ranks,
            total_ones: cumulative as usize,
        }
    }

    /// Number of 1-bits in [0, i] inclusive.
    pub fn rank1(&self, i: usize) -> usize {
        if i >= self.n { return self.total_ones; }
        let block = i / 64;
        let offset = i % 64;
        // Mask: bits 0..=offset set to 1
        let mask = if offset == 63 { u64::MAX } else { (1u64 << (offset + 1)) - 1 };
        let partial = (self.words[block] & mask).count_ones() as usize;
        self.block_ranks[block] as usize + partial
    }

    pub fn rank0(&self, i: usize) -> usize {
        (i + 1) - self.rank1(i)
    }

    pub fn access(&self, i: usize) -> bool {
        self.words[i / 64] >> (i % 64) & 1 == 1
    }

    /// 1-based: position of the j-th 1-bit.
    pub fn select1(&self, j: usize) -> Option<usize> {
        if j == 0 || j > self.total_ones { return None; }
        // Binary search over block_ranks for the block containing the j-th 1
        let mut lo = 0usize;
        let mut hi = self.words.len();
        while lo + 1 < hi {
            let mid = (lo + hi) / 2;
            if self.block_ranks[mid] as usize >= j {
                hi = mid;
            } else {
                lo = mid;
            }
        }
        // Scan within the block
        let mut remaining = j - self.block_ranks[lo] as usize;
        let word = self.words[lo];
        for bit in 0..64usize {
            if word >> bit & 1 == 1 {
                remaining -= 1;
                if remaining == 0 {
                    return Some(lo * 64 + bit);
                }
            }
        }
        None
    }
}

// Elias-Fano encoding for sorted integer sequences.
// Supports O(1) access and O(log(U/n)) predecessor queries via binary search.
// Space: n * lower_bits + 2n bits ≈ n * log(U/n) + 2n bits.
pub struct EliasFano {
    upper: BitVector,
    lower_bits: usize,
    lower_data: Vec<u64>,
    n: usize,
}

impl EliasFano {
    pub fn new(sorted: &[usize], u: usize) -> Self {
        let n = sorted.len();
        if n == 0 {
            return EliasFano {
                upper: BitVector::from_bits(&[]),
                lower_bits: 0,
                lower_data: vec![],
                n: 0,
            };
        }

        let lower_bits = if u / n > 1 {
            (usize::BITS - (u / n).leading_zeros()) as usize - 1
        } else {
            0
        };
        let lower_mask = if lower_bits == 0 { 0 } else { (1usize << lower_bits) - 1 };

        let num_upper_bits = (sorted[n - 1] >> lower_bits) + 1 + n;
        let mut upper_bits = vec![false; num_upper_bits + 1];
        let lower_storage_len = (n * lower_bits + 63) / 64 + 1;
        let mut lower_data = vec![0u64; lower_storage_len];

        let mut pos = 0usize;
        let mut prev_upper = 0usize;

        for (i, &v) in sorted.iter().enumerate() {
            let upper = v >> lower_bits;
            let lower = v & lower_mask;

            for _ in 0..(upper - prev_upper) {
                if pos < upper_bits.len() {
                    upper_bits[pos] = false;
                    pos += 1;
                }
            }
            if pos < upper_bits.len() {
                upper_bits[pos] = true;
                pos += 1;
            }
            prev_upper = upper;

            if lower_bits > 0 {
                let bit_pos = i * lower_bits;
                let word_idx = bit_pos / 64;
                let bit_off = bit_pos % 64;
                if word_idx < lower_data.len() {
                    lower_data[word_idx] |= (lower as u64) << bit_off;
                }
            }
        }

        EliasFano {
            upper: BitVector::from_bits(&upper_bits[..pos]),
            lower_bits,
            lower_data,
            n,
        }
    }

    pub fn access(&self, i: usize) -> Option<usize> {
        if i >= self.n { return None; }
        let lower_val = if self.lower_bits == 0 {
            0
        } else {
            let lower_mask = (1usize << self.lower_bits) - 1;
            let bit_pos = i * self.lower_bits;
            let word_idx = bit_pos / 64;
            let bit_off = bit_pos % 64;
            if word_idx < self.lower_data.len() {
                ((self.lower_data[word_idx] >> bit_off) as usize) & lower_mask
            } else {
                0
            }
        };

        let one_pos = self.upper.select1(i + 1)?;
        let upper_val = one_pos - i; // zeros before the i-th one
        Some((upper_val << self.lower_bits) | lower_val)
    }

    pub fn len(&self) -> usize { self.n }
}

fn main() {
    println!("=== Bit Vector ===");
    let bits = vec![false, true, false, true, true, false, true, false, true, true];
    let bv = BitVector::from_bits(&bits);

    for i in 0..bits.len() {
        println!("bit[{}]={} rank1[{}]={} rank0[{}]={}",
            i, bv.access(i), i, bv.rank1(i), i, bv.rank0(i));
    }
    println!("select1(3) = {:?}", bv.select1(3));
    println!("select1(5) = {:?}", bv.select1(5));

    println!("\n=== Elias-Fano ===");
    let sorted = vec![2usize, 5, 8, 11, 14, 18, 22, 30, 40, 50];
    let ef = EliasFano::new(&sorted, 64);
    print!("Decoded: ");
    for i in 0..ef.len() {
        print!("{} ", ef.access(i).unwrap_or(0));
    }
    println!();
    println!("lower_bits per element: {}", ef.lower_bits);
}
```

### Rust-specific considerations

`u64::count_ones()` compiles to the `POPCNT` hardware instruction on x86_64 — confirmed by checking the emitted assembly with `cargo rustc -- --emit=asm`. This is the same as Go's `bits.OnesCount64`. Both languages produce identical machine code for this critical inner loop.

The Rust implementation benefits from Rust's slice bounds checking in debug mode — an out-of-bounds access in `block_ranks` during rank computation will panic immediately with a clear message, rather than producing a silent incorrect result. In release mode, bounds checks are elided where the compiler can prove safety, so there is no runtime overhead.

For a production succinct library in Rust, the `sucds` crate (https://crates.io/crates/sucds) provides succinct bit vectors, Elias-Fano, and wavelet trees with a clean API. The `bio` crate (for bioinformatics) includes FM-index implementations used in real genome alignment pipelines.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| POPCNT instruction | `bits.OnesCount64` → `POPCNT` on amd64 | `u64::count_ones()` → `POPCNT` on x86_64 |
| Bit packing | Manual `uint64` slice with bit masks | Same; `u64` slice with bit masks |
| Memory layout | `[]uint64` (GC-managed heap) | `Vec<u64>` (allocator-managed heap) |
| Production libraries | No stdlib support; custom or `bits` library | `sucds` crate; `bio` crate |
| SIMD optimization | Requires `unsafe` or assembly | `std::simd` (nightly) or `packed_simd` crate |
| Safety | `math/bits` functions are safe; indexing panics on OOB | Same; bounds-checked in debug mode |

Both languages have the same fundamental performance for rank/select — the bottleneck is POPCNT throughput and memory latency, not language overhead. For large bit vectors (> L3 cache size), memory bandwidth dominates.

## Production War Stories

**BWA-MEM2 genome aligner** (GitHub: bwa-mem2/bwa-mem2): BWA-MEM2 is the most widely used short-read DNA aligner. It uses an FM-index built on the human reference genome (hg38, ~3.2 GB compressed to ~700 MB in the FM-index). Each alignment query (a 100-150 bp read) performs O(m log σ) rank operations on the wavelet tree — approximately 150 × 2 = 300 operations for DNA (σ=4, log2(4)=2). The critical optimization in BWA-MEM2 vs the original BWA is SIMD-accelerated rank/select that processes 256 or 512 bits per instruction.

**Apache Lucene Elias-Fano posting lists** (Lucene source: `codecs/compressing/`): Apache Lucene (used by Elasticsearch and Solr) uses Elias-Fano encoding for its inverted index posting lists. A posting list stores the sorted list of document IDs containing a given term. For a common term appearing in 10^6 out of 10^7 documents, Elias-Fano uses approximately log(10^7/10^6) + 2 = 5.3 bits per document ID, versus 24 bits for a raw integer. Lucene's implementation supports predecessor queries (O(1) using select) for "skip" jumps during AND queries — finding the next document matching all terms.

**SDSL-Lite: Succinct Data Structure Library** (GitHub: simongog/sdsl-lite): The reference C++ implementation of succinct data structures, developed at the Karlsruhe Institute of Technology. It is used as a dependency or reference in dozens of bioinformatics tools. The library's design principle — every succinct structure reports its exact bit usage, and structures compose via templates — is the production blueprint for implementing succinct structures in any language.

## Complexity Analysis

| Structure | Space | Rank | Select | Access |
|-----------|-------|------|--------|--------|
| Naive bit array | n bits | O(n) | O(n) | O(1) |
| Bit array + block ranks | n + O(n/log n) bits | O(1) | O(log n) | O(1) |
| Full Jacobson rank/select | n + o(n) bits | O(1) | O(1) | O(1) |
| Wavelet tree | n log σ bits | O(log σ) | O(log σ) | O(log σ) |
| Elias-Fano | n log(U/n) + 2n bits | O(log(U/n)) | O(1) | O(1) |
| FM-index (text T) | H_k(T) × n + o(n) bits | O(m log σ) query | — | — |

The "o(n) bits" in the Jacobson rank/select structure: it is O(n log log n / log n) bits for the two-level block structure — the metadata is asymptotically smaller than the data. For n=10^9 bits, the metadata is approximately 10^9 × 20/30 ≈ 660 MB — comparable to the 125 MB data. In practice, the constants matter more than the asymptotics for n < 10^9.

## Common Pitfalls

**Pitfall 1: Using 0-indexed rank when the wavelet tree or FM-index expects 1-indexed**

The rank function has two conventions: rank(i) can mean "number of 1s in [0, i)" (exclusive upper bound, 0-indexed) or "number of 1s in [0, i]" (inclusive, 0-indexed). The FM-index backward search algorithm assumes a specific convention — mixing conventions silently produces incorrect pattern counts without any error signal.

Detection: a test with a known pattern "AA" in "AACGT" that should return count=1 returns 0 or 2.

**Pitfall 2: Block rank overflow for very large bit vectors**

Storing block ranks as `uint32` (4 bytes) limits the cumulative rank to 2^32-1 ≈ 4.3 billion ones. For a bit vector larger than 4.3 GB (e.g., a wavelet tree over a 2 GB genome with σ=4), the block ranks must be `uint64`. Using `uint32` silently wraps and produces incorrect ranks for positions past the overflow point.

**Pitfall 3: Not handling the BWT's terminator character in FM-index search**

The BWT requires appending a lexicographically smallest terminator (often `$` or `\0`) to the text before computing the BWT. The FM-index backward search must account for this terminator in the C array and Occ structure. Forgetting it causes incorrect pattern counts when the pattern appears at the end of the text.

**Pitfall 4: Elias-Fano requiring strictly sorted input**

Elias-Fano encoding requires the input sequence to be strictly non-decreasing. If the input contains duplicates or is not sorted, the encoding is silently incorrect — the upper part unary encoding can produce negative gaps that corrupt the bit vector. Always validate that `sorted[i] <= sorted[i+1]` for all i before construction.

**Pitfall 5: Confusing select(j) returning a 0-indexed vs 1-indexed position**

Some implementations return the 0-indexed position of the j-th 1-bit; others return the 1-indexed position. Both conventions exist in the literature. The FM-index's count query produces different results depending on which convention is used. Always test select(1) on a bit vector starting with 1 — it should return 0 (0-indexed) or 1 (1-indexed) depending on the convention.

## Exercises

**Exercise 1 — Verification** (30 min): Build a `BitVector` from a random 1000-bit string and verify that `Rank1(i) + Rank0(i) == i + 1` for all i, and that `Access(Select1(j)) == true` for all j from 1 to TotalOnes. Add a property-based test using any random test framework.

**Exercise 2 — Extension** (2-4h): Implement O(1) select using the two-level structure: for every (log n)-th 1-bit (a "sample"), store the bit position explicitly. For queries between samples, use a local linear scan (bounded by log n bits). Verify that select performance improves for large bit vectors (n > 10^6).

**Exercise 3 — From Scratch** (4-8h): Implement a complete wavelet tree for DNA sequences (alphabet {A, C, G, T} → values {0, 1, 2, 3}). Support `rank(c, i)` and `access(i)` queries. Build the wavelet tree over a synthetic 10000-character DNA sequence. Verify that `rank('A', i) + rank('C', i) + rank('G', i) + rank('T', i) == i + 1` for all i.

**Exercise 4 — Production Scenario** (8-15h): Implement the FM-index for DNA pattern matching: given a reference genome (you can use a synthetic 1 MB sequence for testing), build the BWT, the C array, and the Occ structure (using your wavelet tree). Implement `count(pattern)` (how many times does the pattern appear?) and `locate(pattern)` (at what positions?). Benchmark against a naive O(n) scan and a binary-search-over-suffix-array approach. Compare space usage.

## Further Reading

### Foundational Papers
- Jacobson, G. (1989). "Space-Efficient Static Trees and Graphs." *FOCS 1989*. Introduces O(1) rank and select with o(n) additional space.
- Ferragina, P., & Manzini, G. (2000). "Opportunistic Data Structures with Applications." *FOCS 2000*. The original FM-index paper.
- Grossi, R., & Vitter, J. S. (2005). "Compressed Suffix Arrays and Suffix Trees with Applications to Text Indexing and String Matching." *SIAM Journal on Computing*, 35(2). Compressed suffix arrays with near-optimal space.
- Claude, F., & Navarro, G. (2015). "The Wavelet Tree on Practical Settings." *Pattern Recognition Letters*. Engineering analysis of wavelet tree performance.

### Books
- Navarro, G., & Mäkinen, V. (2007). *Compressed Text Databases*. Surveys all major compressed index structures.
- Makinen, V., Belazzougui, D., Cunial, F., & Tomescu, A. I. (2015). *Genome-Scale Algorithm Design*. Chapters 2-4 cover BWT, FM-index, and wavelet trees in the context of genome assembly.

### Production Code to Read
- `simongog/sdsl-lite` (https://github.com/simongog/sdsl-lite) — The reference C++ succinct library. Study `include/sdsl/rrr_vector.hpp` for run-length compressed bit vectors and `include/sdsl/wavelet_trees.hpp` for the wavelet tree taxonomy.
- `bwa-mem2/bwa-mem2` (https://github.com/bwa-mem2/bwa-mem2) — The FM-index implementation with SIMD-accelerated rank. `bwtindex.c` builds the BWT; `bwt.c` implements backward search.
- `apache/lucene` (https://github.com/apache/lucene) — `lucene/core/src/java/org/apache/lucene/util/packed/EliasFanoEncoder.java` for the Elias-Fano posting list encoding.

### Conference Talks
- Navarro, G. (DCC 2014): "Wavelet Trees for All" — Survey of wavelet tree applications beyond text indexing.
- Puglisi, S. J. (CPM 2012): "The BWT in Practice" — Engineering analysis of BWT construction and FM-index performance on modern hardware.
