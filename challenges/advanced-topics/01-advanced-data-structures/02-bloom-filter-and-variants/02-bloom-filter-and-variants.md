<!--
type: reference
difficulty: advanced
section: [01-advanced-data-structures]
concepts: [bloom-filter, counting-bloom-filter, cuckoo-filter, xor-filter, false-positive-rate, hash-functions]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: analyze
prerequisites: [hash-functions, bit-manipulation, probability-basic]
papers: [bloom-1970-space-time, fan-2014-cuckoo-filter, graf-2020-xor-filter]
industry_use: [rocksdb-sstable-lookup, cassandra-read-path, postgresql-bloom-indexes, chrome-safe-browsing]
language_contrast: medium
-->

# Bloom Filter and Variants

> RocksDB uses a Bloom filter at every SSTable level to avoid disk reads for keys that don't exist — without it, a `Get` on a cold key would require reading every SSTable file on disk.

## Mental Model

A Bloom filter answers the question "have I seen this item before?" with a probabilistic guarantee: it never produces false negatives (if it says "no," the item is definitely absent) but can produce false positives (if it says "yes," the item might or might not be present). The fundamental trade is accuracy for space: a Bloom filter represents a set of n items using only m bits, where m << n × (size of an item).

The practical mental model for production use is this: a Bloom filter is a cache of negative answers. When you need to look up a key in an expensive storage layer (disk, network, another service), you first check the Bloom filter. If it says "no," you skip the expensive lookup entirely. If it says "maybe yes," you do the lookup. The false positive rate determines how often you do an unnecessary expensive lookup, and you tune m and k (number of hash functions) to achieve the false positive rate that balances storage cost against lookup cost.

The variants exist because the standard Bloom filter has two critical limitations: it cannot delete elements, and it cannot be resized without rebuilding. Every variant trades something to address one of these:

- **Counting Bloom filter**: replaces bits with counters, enabling deletion at the cost of 4-8x more space.
- **Cuckoo filter**: uses cuckoo hashing with fingerprints, supporting deletion and typically better space efficiency than Bloom at low false positive rates.
- **XOR filter**: static (no insertions after build), but achieves near-optimal space — roughly 1.23 bits per element for 1% FPR, versus ~9.6 bits for Bloom — by using a different lookup structure built offline.

## Core Concepts

### Standard Bloom Filter Mathematics

A Bloom filter uses k independent hash functions mapping each element to k positions in a bit array of length m. To insert, set those k bits to 1. To query, check all k bits: if any is 0, the element is absent; if all are 1, the element is probably present.

The false positive probability for n inserted elements is approximately:

`FPR ≈ (1 - e^(-kn/m))^k`

The optimal number of hash functions that minimizes FPR for a given m/n ratio is:

`k_opt = (m/n) × ln(2) ≈ 0.693 × (m/n)`

With k=k_opt, the FPR simplifies to:

`FPR ≈ (0.6185)^(m/n)`

From this you can derive the bits-per-element required for a target FPR:

`m/n = -log2(FPR) / ln(2) ≈ -1.44 × log2(FPR)`

For FPR=1% (0.01): m/n ≈ 9.6 bits per element, k≈7.
For FPR=0.1% (0.001): m/n ≈ 14.4 bits per element, k≈10.

These numbers are your mental anchors: if someone claims a Bloom filter stores a million items with 1% FPR in less than 1.2 MB, they are wrong.

### Why k-independence Matters

Most real implementations use a single 64-bit hash and derive k positions by splitting the hash (Kirsch-Mitzenmacher technique): position_i = (h1 + i × h2) mod m, where h1 and h2 are the upper and lower 32 bits of the 64-bit hash. This gives k hash functions with only 2 actual hash evaluations per operation. The theoretical requirement is k-independence, but in practice even 2-independent hash functions achieve the expected FPR for most use cases. The pathological case is adversarial inputs — if an attacker can choose items, a non-cryptographic hash function may produce correlated results that inflate the FPR.

### Cuckoo Filter and Fingerprints

A Cuckoo filter stores fingerprints (short hashes of items, typically 4-12 bits) in a compact hash table using cuckoo hashing. Each item maps to two candidate buckets; if both are full, it displaces an existing fingerprint and tries to relocate it. Lookup checks both candidate buckets for the fingerprint. Deletion is possible because the fingerprint is stored explicitly — you can remove it directly.

The space efficiency advantage over Bloom filters comes from the fact that a Cuckoo filter's FPR depends primarily on the fingerprint length f: `FPR ≈ 2f/2^f`. At f=8 bits, FPR≈0.78%. To achieve the same FPR, a Cuckoo filter uses approximately 1.05× the theoretical minimum entropy-based space, while a Bloom filter uses ≈1.44×. The disadvantage is that cuckoo hashing can fail to insert (when the displacement chain exceeds a maximum length) — in practice this occurs with probability < 3% at 95% load factor.

### XOR Filter: Static but Near-Optimal

An XOR filter is built offline from the complete set of items. It uses a three-level bucket structure with XOR operations to store fingerprints such that a lookup requires exactly three memory reads (one per level). The build algorithm guarantees that for most inputs, it succeeds; for rare inputs that produce cycles, it retries with a different hash seed.

The key property is that an XOR filter achieves ≈1.23 bits per element for 1% FPR — close to the information-theoretic minimum of `-log2(FPR)/ln(2) ≈ 1.44 bits/element`. This is 7.8x more space-efficient than a Bloom filter at the same FPR. The cost: no insertions or deletions after construction. Use XOR filters when the set is known at build time (e.g., indexing a completed SSTable on disk).

## Implementation: Go

```go
package main

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"math/bits"
)

// BloomFilter is a standard Bloom filter backed by a compact bit array.
// Design choice: we store bits in uint64 words for efficient bulk operations.
type BloomFilter struct {
	bits   []uint64
	m      uint64 // total number of bits
	k      uint   // number of hash functions
	n      uint64 // number of items inserted (for FPR estimation)
}

// NewBloomFilter creates a Bloom filter for the given expected number of items
// and target false positive rate. It computes optimal m and k.
func NewBloomFilter(expectedItems uint64, targetFPR float64) *BloomFilter {
	// m = -n * ln(FPR) / (ln(2)^2)
	m := uint64(math.Ceil(-float64(expectedItems) * math.Log(targetFPR) / (math.Ln2 * math.Ln2)))
	// Round up to nearest 64 for word alignment
	m = ((m + 63) / 64) * 64
	// k = (m/n) * ln(2)
	k := uint(math.Round(float64(m) / float64(expectedItems) * math.Ln2))
	if k < 1 {
		k = 1
	}
	return &BloomFilter{
		bits: make([]uint64, m/64),
		m:    m,
		k:    k,
	}
}

// hashPositions returns k bit positions for the given key using the
// Kirsch-Mitzenmacher double-hashing technique (only 2 hash evaluations).
func (bf *BloomFilter) hashPositions(key []byte) []uint64 {
	// Two independent 64-bit hashes via FNV-1a with different seeds
	h := fnv.New64a()
	h.Write(key)
	h1 := h.Sum64()

	// XOR with a constant to get a second independent hash
	// This is a common approximation; for adversarial inputs use SipHash.
	h2 := h1 ^ 0x9e3779b97f4a7c15 // golden ratio constant

	positions := make([]uint64, bf.k)
	for i := uint(0); i < bf.k; i++ {
		// Combine h1 and h2 to derive position i
		positions[i] = (h1 + uint64(i)*h2) % bf.m
	}
	return positions
}

func (bf *BloomFilter) Add(key []byte) {
	for _, pos := range bf.hashPositions(key) {
		word := pos / 64
		bit := pos % 64
		bf.bits[word] |= 1 << bit
	}
	bf.n++
}

func (bf *BloomFilter) Contains(key []byte) bool {
	for _, pos := range bf.hashPositions(key) {
		word := pos / 64
		bit := pos % 64
		if bf.bits[word]&(1<<bit) == 0 {
			return false // definite absence
		}
	}
	return true // probable presence
}

// FillRatio returns the fraction of bits set to 1.
// FillRatio > 0.5 indicates FPR is degrading; > 0.9 means the filter is nearly useless.
func (bf *BloomFilter) FillRatio() float64 {
	var setBits uint64
	for _, word := range bf.bits {
		setBits += uint64(bits.OnesCount64(word))
	}
	return float64(setBits) / float64(bf.m)
}

// EstimatedFPR returns the current estimated false positive rate given n insertions.
func (bf *BloomFilter) EstimatedFPR() float64 {
	// FPR ≈ (fill_ratio)^k
	return math.Pow(bf.FillRatio(), float64(bf.k))
}

// CountingBloomFilter supports deletion by using 4-bit counters instead of 1-bit flags.
// Space cost: 4x a standard Bloom filter, since each cell needs 4 bits.
// Overflow handling: if a counter reaches 15, we saturate (no further increments).
// This means an element added >15 times cannot be fully deleted — acceptable in practice.
type CountingBloomFilter struct {
	counters []uint8 // each byte holds two 4-bit counters (nibble packing)
	m        uint64
	k        uint
}

func NewCountingBloomFilter(expectedItems uint64, targetFPR float64) *CountingBloomFilter {
	m := uint64(math.Ceil(-float64(expectedItems) * math.Log(targetFPR) / (math.Ln2 * math.Ln2)))
	m = ((m + 1) / 2) * 2 // round up to even for nibble packing
	k := uint(math.Round(float64(m) / float64(expectedItems) * math.Ln2))
	if k < 1 {
		k = 1
	}
	return &CountingBloomFilter{
		counters: make([]uint8, m/2), // two counters per byte
		m:        m,
		k:        k,
	}
}

func (cbf *CountingBloomFilter) getCounter(pos uint64) uint8 {
	b := cbf.counters[pos/2]
	if pos%2 == 0 {
		return b & 0x0F // low nibble
	}
	return (b >> 4) & 0x0F // high nibble
}

func (cbf *CountingBloomFilter) setCounter(pos uint64, val uint8) {
	if val > 15 {
		val = 15 // saturate at 15
	}
	i := pos / 2
	if pos%2 == 0 {
		cbf.counters[i] = (cbf.counters[i] & 0xF0) | (val & 0x0F)
	} else {
		cbf.counters[i] = (cbf.counters[i] & 0x0F) | ((val & 0x0F) << 4)
	}
}

func (cbf *CountingBloomFilter) hashPositions(key []byte) []uint64 {
	h := fnv.New64a()
	h.Write(key)
	h1 := h.Sum64()
	h2 := h1 ^ 0x9e3779b97f4a7c15

	positions := make([]uint64, cbf.k)
	for i := uint(0); i < cbf.k; i++ {
		positions[i] = (h1 + uint64(i)*h2) % cbf.m
	}
	return positions
}

func (cbf *CountingBloomFilter) Add(key []byte) {
	for _, pos := range cbf.hashPositions(key) {
		c := cbf.getCounter(pos)
		cbf.setCounter(pos, c+1)
	}
}

func (cbf *CountingBloomFilter) Remove(key []byte) {
	// Only remove if all counters are > 0 — avoid underflow on elements never added.
	positions := cbf.hashPositions(key)
	for _, pos := range positions {
		if cbf.getCounter(pos) == 0 {
			// This key was never added (or was added and removed already); skip.
			return
		}
	}
	for _, pos := range positions {
		c := cbf.getCounter(pos)
		if c < 15 { // do not decrement saturated counters
			cbf.setCounter(pos, c-1)
		}
	}
}

func (cbf *CountingBloomFilter) Contains(key []byte) bool {
	for _, pos := range cbf.hashPositions(key) {
		if cbf.getCounter(pos) == 0 {
			return false
		}
	}
	return true
}

// fingerprint computes a short fingerprint for use in a Cuckoo filter.
// Must be non-zero: a zero fingerprint would be indistinguishable from an empty slot.
func fingerprint(key []byte, bits uint) uint64 {
	h := fnv.New64a()
	h.Write(key)
	fp := h.Sum64() & ((1 << bits) - 1)
	if fp == 0 {
		fp = 1 // zero fingerprint reserved for empty slots
	}
	return fp
}

// CuckooFilter supports deletion and achieves better space efficiency than Bloom
// at false positive rates below ~2%. Buckets have 4 slots each (standard design).
type CuckooFilter struct {
	buckets     [][]uint64
	numBuckets  uint64
	fpBits      uint   // fingerprint length in bits
	bucketSize  int    // slots per bucket (4 is standard)
	count       uint64
	maxKickouts int
}

func NewCuckooFilter(capacity uint64, fpBits uint) *CuckooFilter {
	// Number of buckets: capacity / bucketSize, rounded up to power of 2
	bucketSize := 4
	numBuckets := uint64(1)
	for numBuckets*uint64(bucketSize) < capacity {
		numBuckets <<= 1
	}
	buckets := make([][]uint64, numBuckets)
	for i := range buckets {
		buckets[i] = make([]uint64, bucketSize)
	}
	return &CuckooFilter{
		buckets:     buckets,
		numBuckets:  numBuckets,
		fpBits:      fpBits,
		bucketSize:  bucketSize,
		maxKickouts: 500, // empirically: 500 kickouts is enough for 95% load
	}
}

func (cf *CuckooFilter) bucketIndex(key []byte) uint64 {
	h := fnv.New64a()
	h.Write(key)
	return h.Sum64() % cf.numBuckets
}

func (cf *CuckooFilter) altIndex(i1 uint64, fp uint64) uint64 {
	// The alternate bucket is computed from the fingerprint alone so that
	// we can compute it from either bucket without the original key.
	// This is the key property that enables deletion in Cuckoo filters.
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, fp)
	h := fnv.New64a()
	h.Write(b)
	return (i1 ^ h.Sum64()) % cf.numBuckets
}

func (cf *CuckooFilter) insertIntoBucket(bIdx uint64, fp uint64) bool {
	for j := 0; j < cf.bucketSize; j++ {
		if cf.buckets[bIdx][j] == 0 {
			cf.buckets[bIdx][j] = fp
			return true
		}
	}
	return false // bucket full
}

func (cf *CuckooFilter) Add(key []byte) bool {
	fp := fingerprint(key, cf.fpBits)
	i1 := cf.bucketIndex(key)
	i2 := cf.altIndex(i1, fp)

	if cf.insertIntoBucket(i1, fp) || cf.insertIntoBucket(i2, fp) {
		cf.count++
		return true
	}

	// Both buckets full: kick out a random entry and relocate it
	curIdx := i1
	for k := 0; k < cf.maxKickouts; k++ {
		// Pick a random slot in the current bucket to displace
		slot := k % cf.bucketSize
		displaced := cf.buckets[curIdx][slot]
		cf.buckets[curIdx][slot] = fp
		fp = displaced
		curIdx = cf.altIndex(curIdx, fp)
		if cf.insertIntoBucket(curIdx, fp) {
			cf.count++
			return true
		}
	}
	// Insertion failed: filter is too full. In production: rebuild with larger capacity.
	return false
}

func (cf *CuckooFilter) Contains(key []byte) bool {
	fp := fingerprint(key, cf.fpBits)
	i1 := cf.bucketIndex(key)
	i2 := cf.altIndex(i1, fp)

	for j := 0; j < cf.bucketSize; j++ {
		if cf.buckets[i1][j] == fp || cf.buckets[i2][j] == fp {
			return true
		}
	}
	return false
}

func (cf *CuckooFilter) Delete(key []byte) bool {
	fp := fingerprint(key, cf.fpBits)
	i1 := cf.bucketIndex(key)
	i2 := cf.altIndex(i1, fp)

	for j := 0; j < cf.bucketSize; j++ {
		if cf.buckets[i1][j] == fp {
			cf.buckets[i1][j] = 0
			cf.count--
			return true
		}
	}
	for j := 0; j < cf.bucketSize; j++ {
		if cf.buckets[i2][j] == fp {
			cf.buckets[i2][j] = 0
			cf.count--
			return true
		}
	}
	return false
}

func main() {
	fmt.Println("=== Standard Bloom Filter ===")
	bf := NewBloomFilter(10000, 0.01)
	fmt.Printf("m=%d bits, k=%d hash functions\n", bf.m, bf.k)

	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("item-%d", i))
		bf.Add(key)
	}
	fmt.Printf("Fill ratio: %.3f\n", bf.FillRatio())
	fmt.Printf("Estimated FPR: %.4f\n", bf.EstimatedFPR())

	// Count false positives from items never inserted
	var fp int
	for i := 10000; i < 20000; i++ {
		key := []byte(fmt.Sprintf("item-%d", i))
		if bf.Contains(key) {
			fp++
		}
	}
	fmt.Printf("Measured FPR: %.4f\n", float64(fp)/10000.0)

	fmt.Println("\n=== Counting Bloom Filter (with deletion) ===")
	cbf := NewCountingBloomFilter(1000, 0.01)
	cbf.Add([]byte("key-a"))
	cbf.Add([]byte("key-b"))
	fmt.Printf("Contains key-a before remove: %v\n", cbf.Contains([]byte("key-a")))
	cbf.Remove([]byte("key-a"))
	fmt.Printf("Contains key-a after remove: %v\n", cbf.Contains([]byte("key-a")))
	fmt.Printf("Contains key-b (unaffected): %v\n", cbf.Contains([]byte("key-b")))

	fmt.Println("\n=== Cuckoo Filter ===")
	cf := NewCuckooFilter(10000, 8) // 8-bit fingerprints → FPR ≈ 0.78%
	var insertFailed int
	for i := 0; i < 8000; i++ {
		key := []byte(fmt.Sprintf("item-%d", i))
		if !cf.Add(key) {
			insertFailed++
		}
	}
	fmt.Printf("Items inserted: %d, insert failures: %d\n", cf.count, insertFailed)
	fmt.Printf("Contains item-0: %v\n", cf.Contains([]byte("item-0")))
	cf.Delete([]byte("item-0"))
	fmt.Printf("Contains item-0 after delete: %v\n", cf.Contains([]byte("item-0")))
}
```

### Go-specific considerations

The `encoding/binary` and `hash/fnv` packages from the standard library are sufficient for a Bloom filter; `fnv.New64a` (FNV-1a, 64-bit) is fast and well-distributed for byte slices. For production use at scale, replace with `xxhash` or `SipHash-1-3` — the latter is required when inputs are adversary-controlled (e.g., user-submitted strings that could be crafted to maximize collisions).

Bit packing with `uint64` slices is important for performance: Go's garbage collector must scan pointers in heap objects, and a `[]bool` or `[]uint8` filter will have the GC scanning n/8 to n words respectively. A `[]uint64` filter of m/64 words is both more compact and has a smaller GC scan cost.

The `CountingBloomFilter` nibble packing (two 4-bit counters per byte) halves the counter array size versus `[]uint8`. For very large filters (m > 10^9 bits), consider `[]uint32` with 8-bit counters packed 4 per word, or use a file-backed `mmap` region with the same packing.

## Implementation: Rust

```rust
use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};

// Bloom filter with optimized bit packing.
// The Rust version demonstrates using const generics for compile-time capacity,
// which is appropriate for filters embedded in other structures (e.g., SSTable block metadata).

pub struct BloomFilter {
    bits: Vec<u64>,
    m: usize,
    k: usize,
    n: usize,
}

impl BloomFilter {
    pub fn new(expected_items: usize, target_fpr: f64) -> Self {
        let ln2 = std::f64::consts::LN_2;
        let m = (-(expected_items as f64) * target_fpr.ln() / (ln2 * ln2)).ceil() as usize;
        let m = ((m + 63) / 64) * 64; // align to 64 bits
        let k = ((m as f64 / expected_items as f64) * ln2).round() as usize;
        let k = k.max(1);
        BloomFilter {
            bits: vec![0u64; m / 64],
            m,
            k,
            n: 0,
        }
    }

    // Double hashing using std DefaultHasher for simplicity.
    // Production: use xxhash or ahash — both are significantly faster and
    // have better distribution than the default hasher.
    fn hash_positions(&self, key: &[u8]) -> Vec<usize> {
        // h1: hash the key directly
        let mut h = DefaultHasher::new();
        key.hash(&mut h);
        let h1 = h.finish();

        // h2: hash the key with a different seed (simulate by hashing h1)
        let mut h2_hasher = DefaultHasher::new();
        h1.hash(&mut h2_hasher);
        let h2 = h2_hasher.finish();

        (0..self.k)
            .map(|i| (h1.wrapping_add(i as u64 * h2) as usize) % self.m)
            .collect()
    }

    pub fn add(&mut self, key: &[u8]) {
        for pos in self.hash_positions(key) {
            let word = pos / 64;
            let bit = pos % 64;
            self.bits[word] |= 1u64 << bit;
        }
        self.n += 1;
    }

    pub fn contains(&self, key: &[u8]) -> bool {
        self.hash_positions(key).iter().all(|&pos| {
            let word = pos / 64;
            let bit = pos % 64;
            self.bits[word] & (1u64 << bit) != 0
        })
    }

    pub fn fill_ratio(&self) -> f64 {
        let set_bits: u32 = self.bits.iter().map(|w| w.count_ones()).sum();
        set_bits as f64 / self.m as f64
    }

    pub fn estimated_fpr(&self) -> f64 {
        self.fill_ratio().powi(self.k as i32)
    }

    pub fn m(&self) -> usize { self.m }
    pub fn k(&self) -> usize { self.k }
}

// XOR filter — static, near-optimal space, exactly 3 memory accesses per lookup.
// Build algorithm: "Xor Filters: Faster and Smaller Than Bloom and Cuckoo Filters"
// (Graf & Lemire, 2020, Journal of Experimental Algorithmics).
pub struct XorFilter {
    fingerprints: Vec<u8>, // 8-bit fingerprints; use u16 for lower FPR
    block_length: usize,
    // Seed for the hash family; retried on build failure
    seed: u64,
}

impl XorFilter {
    // Build an XOR filter from a slice of keys.
    // Returns None only if the build failed after max retries (extremely rare).
    pub fn build(keys: &[u64]) -> Option<Self> {
        let n = keys.len();
        // block_length must be slightly larger than n/3 for the three-level structure
        let block_length = (n as f64 * 1.23).ceil() as usize + 32;
        let capacity = 3 * block_length;

        for seed in 0..100u64 {
            let mut fingerprints = vec![0u8; capacity];
            // Phase 1: compute hash sets — which positions each key maps to
            let mut h0 = vec![0usize; n];
            let mut h1 = vec![0usize; n];
            let mut h2 = vec![0usize; n];
            let mut xor_mask = vec![0u8; capacity];
            let mut count = vec![0u32; capacity];

            for (i, &key) in keys.iter().enumerate() {
                let hashes = Self::key_hashes(key, seed, block_length);
                h0[i] = hashes[0];
                h1[i] = hashes[1];
                h2[i] = hashes[2];
                let fp = Self::fingerprint(key, seed);
                xor_mask[hashes[0]] ^= fp;
                xor_mask[hashes[1]] ^= fp;
                xor_mask[hashes[2]] ^= fp;
                count[hashes[0]] += 1;
                count[hashes[1]] += 1;
                count[hashes[2]] += 1;
            }

            // Phase 2: peel — find positions that appear in exactly one key's hash set
            let mut queue: Vec<(usize, usize)> = Vec::new(); // (position, key_idx)
            for i in 0..n {
                for &hpos in &[h0[i], h1[i], h2[i]] {
                    if count[hpos] == 1 {
                        queue.push((hpos, i));
                        break;
                    }
                }
            }

            if queue.is_empty() && n > 0 {
                continue; // seed doesn't work, try next
            }

            // Phase 3: assign fingerprints in reverse peeling order
            let mut order = Vec::with_capacity(n);
            let mut qi = 0;
            while qi < queue.len() {
                let (pos, key_idx) = queue[qi];
                qi += 1;
                order.push((pos, key_idx));
                for &hpos in &[h0[key_idx], h1[key_idx], h2[key_idx]] {
                    count[hpos] = count[hpos].saturating_sub(1);
                    if count[hpos] == 1 {
                        // Find which key still maps here
                        for (j, &k) in keys.iter().enumerate() {
                            let hj = Self::key_hashes(k, seed, block_length);
                            if (hj[0] == hpos || hj[1] == hpos || hj[2] == hpos)
                                && j != key_idx
                            {
                                queue.push((hpos, j));
                                break;
                            }
                        }
                    }
                }
            }

            if order.len() != n {
                continue; // build failed, retry with different seed
            }

            // Assign fingerprints in reverse order
            for &(pos, key_idx) in order.iter().rev() {
                let fp = Self::fingerprint(keys[key_idx], seed);
                let hashes = Self::key_hashes(keys[key_idx], seed, block_length);
                let other_xor: u8 = hashes
                    .iter()
                    .filter(|&&h| h != pos)
                    .map(|&h| fingerprints[h])
                    .fold(0, |acc, v| acc ^ v);
                fingerprints[pos] = fp ^ other_xor;
            }
            let _ = xor_mask; // no longer needed

            return Some(XorFilter { fingerprints, block_length, seed });
        }
        None
    }

    fn key_hashes(key: u64, seed: u64, block_length: usize) -> [usize; 3] {
        // Three hash functions using different mixing constants
        let h = key.wrapping_mul(0x9e3779b97f4a7c15).wrapping_add(seed);
        let h0 = (h ^ (h >> 33)) as usize % block_length;
        let h1 = ((h ^ (h >> 17)).wrapping_mul(0x6c62272e07bb0142)) as usize % block_length
            + block_length;
        let h2 = ((h ^ (h >> 29)).wrapping_mul(0x94d049bb133111eb)) as usize % block_length
            + 2 * block_length;
        [h0, h1, h2]
    }

    fn fingerprint(key: u64, seed: u64) -> u8 {
        let h = key.wrapping_mul(0xbf58476d1ce4e5b9).wrapping_add(seed);
        ((h ^ (h >> 32)) & 0xFF) as u8
    }

    pub fn contains(&self, key: u64) -> bool {
        let hashes = Self::key_hashes(key, self.seed, self.block_length);
        let fp = Self::fingerprint(key, self.seed);
        // Exactly 3 memory accesses; XOR of the three stored fingerprints must equal fp
        self.fingerprints[hashes[0]]
            ^ self.fingerprints[hashes[1]]
            ^ self.fingerprints[hashes[2]]
            == fp
    }
}

fn main() {
    println!("=== Bloom Filter ===");
    let mut bf = BloomFilter::new(10_000, 0.01);
    println!("m={} bits, k={} hash functions", bf.m(), bf.k());

    for i in 0u32..10_000 {
        bf.add(&i.to_le_bytes());
    }
    println!("Fill ratio: {:.3}", bf.fill_ratio());
    println!("Estimated FPR: {:.4}", bf.estimated_fpr());

    let false_positives: usize = (10_000u32..20_000)
        .filter(|i| bf.contains(&i.to_le_bytes()))
        .count();
    println!("Measured FPR: {:.4}", false_positives as f64 / 10_000.0);

    println!("\n=== XOR Filter ===");
    let keys: Vec<u64> = (0u64..1000).collect();
    match XorFilter::build(&keys) {
        None => println!("XOR filter build failed"),
        Some(xf) => {
            // All inserted keys must be found
            let all_found = keys.iter().all(|&k| xf.contains(k));
            println!("All inserted keys found: {}", all_found);

            // Measure FPR on non-inserted keys
            let fp: usize = (1000u64..11_000).filter(|&k| xf.contains(k)).count();
            println!("XOR filter FPR on 10k non-members: {:.4}", fp as f64 / 10_000.0);
            println!(
                "Filter size: {} bytes ({:.2} bits/element)",
                xf.fingerprints.len(),
                (xf.fingerprints.len() * 8) as f64 / keys.len() as f64
            );
        }
    }
}
```

### Rust-specific considerations

Rust's iterator methods (`.all()`, `.map()`, `.filter()`, `.fold()`) compile to the same code as explicit loops but express intent more clearly. The `contains` method using `.iter().all(|&pos| ...)` is idiomatic and generates no overhead vs. an explicit loop.

For production Bloom filters in Rust, the `bloom` crate provides a well-tested implementation, but many systems embed their own (RocksDB-style) because they need to serialize the filter to disk. The serialization format matters: the bit array must be portable across architectures (use little-endian for the uint64 words), and the hash function must be pinned to a specific version (FNV-1a is stable; `DefaultHasher` is explicitly not stable across Rust versions or platforms).

The XOR filter build is inherently sequential in the implementation above (the peeling algorithm has data dependencies). A parallel build would use a concurrent queue for the peeling phase, but this is rarely worth the complexity — XOR filter builds for SSTable creation happen on the write path, not on the hot read path.

For `unsafe` avoidance: this implementation is entirely safe Rust. The raw bit manipulation uses only integer arithmetic and slice indexing. Bounds panics are possible if the hash functions produce out-of-range positions — the `% self.m` and `% capacity` modulo operations prevent this.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Bit array type | `[]uint64` (heap-allocated, GC-managed) | `Vec<u64>` (heap-allocated, drop-managed) |
| Hash function | `hash/fnv` in stdlib; `xxhash` via module | No fast hash in stdlib; use `ahash` or `xxhash` crate |
| Serialization | Manual byte slice manipulation | `serde` with `#[derive(Serialize)]` or manual |
| Concurrency | Multiple readers: safe with `[]uint64`; writer needs mutex | Same; `Arc<RwLock<BloomFilter>>` or atomic bit ops |
| Filter crates | `willf/bloom` (well-maintained) | `bloom` crate; many embed their own |
| Compile-time sizing | Not possible (no const generics for this) | Possible with `const N: usize` generic parameter |

The most significant production difference is that Rust can create Bloom filters as const-sized stack structures for small filters (e.g., a 64-bit mini-filter for a small hash bucket), while Go always heap-allocates the bit array. For a database that creates per-SSTable-block Bloom filters (RocksDB's block-level filters), avoiding heap allocation per block can meaningfully reduce allocator pressure.

The hash function choice also diverges: Go's `hash/fnv` is part of the stdlib and produces consistent results across versions. Rust's `DefaultHasher` is explicitly documented as not stable — using it in a persistent Bloom filter that survives process restarts will produce incorrect results when the hasher changes. Always pin the hash function for any filter persisted to disk.

## Production War Stories

**RocksDB SSTable Bloom filters** (RocksDB source: `table/block_based/filter_block.cc`): Every RocksDB SSTable includes a Bloom filter (or, since RocksDB 6.6, an optionally enabled Ribbon filter — a variant with better space efficiency). The filter is consulted on every `Get` before any disk read. The false positive rate is tunable via `options.bloom_locality` and `options.bits_per_key`. At 10 bits/key (FPR≈0.83%), RocksDB's benchmarks show Bloom filters eliminating 99%+ of disk reads for random-key workloads where the key is absent. The RocksDB team documented their switch from standard Bloom to a "full filter" (per-SSTable, not per-block) in the blog post "Indexing SST Files for Better Lookup Performance" (2016), showing a 2-4x improvement in read latency for non-existent key lookups.

**Chrome Safe Browsing** (Google, 2012): Chrome uses a Bloom filter to check URLs against Google's list of malicious sites locally before making a network request. The filter holds approximately 650,000 entries with a 1-2% FPR — accepting that 1-2% of safe URLs trigger a network verification round-trip. This illustrates the exact use case Bloom filters are built for: avoid the expensive operation (network request) for the vast majority of cases, paying only for false positives.

**Cassandra read path** (Apache Cassandra source: `bloom_filter` package): Cassandra uses per-SSTable Bloom filters with a configurable FPR (default 1%). Unlike RocksDB which reads filters from disk, Cassandra keeps them in off-heap memory (`sun.misc.Unsafe` allocations, or in modern versions, native memory via JNI) to avoid GC pressure. The space consumed by Bloom filters in a large Cassandra cluster can exceed the size of the working dataset — a real operational concern documented in the Cassandra documentation's "Understanding the SSTable cache" section.

## Complexity Analysis

| Filter Type | Space | Lookup | Insert | Delete |
|-------------|-------|--------|--------|--------|
| Bloom | O(m) | O(k) | O(k) | No |
| Counting Bloom | O(4m) | O(k) | O(k) | O(k) |
| Cuckoo | O(n × f / α) | O(1) amortized | O(1) amortized | O(1) |
| XOR | O(1.23n) | O(1) (3 reads) | Build-only | No |

Where: m = bit array size, k = hash functions, f = fingerprint bits, α = load factor (≈0.95), n = number of items.

Hidden constants: k is typically 7-10, meaning Bloom filter lookups touch 7-10 cache lines for large filters (when m >> L1 cache). Cuckoo filter's O(1) lookup is literally 2 cache line reads (one per candidate bucket). XOR filter's O(1) is 3 reads in predictable positions, enabling hardware prefetching. For large n (>10^7), cache behavior dominates: Cuckoo and XOR win decisively over Bloom for random lookups.

## Common Pitfalls

**Pitfall 1: Using a single hash function split into k weak hashes**

The Kirsch-Mitzenmacher double hashing (h1 + i×h2) is an approximation — it produces k-independent hash values only in a loose sense. For most practical workloads, the approximation is fine. But if the hash function has poor distribution (e.g., a poorly seeded FNV), k positions for a given key may cluster in a small region of the bit array, dramatically increasing the effective FPR. Detection: check the fill ratio histogram — if bits are unevenly distributed across the array, your hash function is biased.

**Pitfall 2: Forgetting that Cuckoo filter insertion can fail**

At high load factors (>95%), Cuckoo filter insertion enters a long kick-out chain and may fail. The caller must handle this — either rebuild the filter with a larger capacity or fall back to a deterministic structure. Many implementations silently discard the item on failure, converting a probabilistic structure (acceptable false positives) into a broken one (silently missing true members, i.e., false negatives).

**Pitfall 3: Using the same hash function family for the Bloom filter and the underlying data store**

If your Bloom filter and your hash table both use FNV-1a, an adversary who knows your hash function can craft inputs that hash to the same positions in both structures, defeating the Bloom filter's effectiveness as a pre-filter and potentially causing hash table collisions simultaneously. Use different hash functions (or different seeds) for each layer.

**Pitfall 4: Not accounting for filter size in memory budget calculations**

A 1% FPR Bloom filter for 10^8 items requires ~120 MB. Teams routinely allocate this during capacity planning, then are surprised when the filter doubles or triples in size because the actual item count exceeded the estimate. Bloom filters do not resize — once m and k are fixed, exceeding n degrades FPR monotonically. Monitor `FillRatio()` and rebuild the filter (or switch to a growing structure like a scalable Bloom filter) before it exceeds 0.7.

**Pitfall 5: Assuming the XOR filter's build always succeeds quickly**

The XOR filter build is probabilistic — it fails if the peeling algorithm gets stuck (a cycle in the hypergraph). With the standard parameters (block_length = 1.23n), build failure probability is < 0.1% per attempt. But for adversarially chosen inputs (e.g., a user controlling key values), an attacker can force repeated build failures by submitting keys that create cycles for common seeds. Use a randomly chosen seed (not a fixed constant) to prevent this.

## Exercises

**Exercise 1 — Verification** (30 min): Run the Go Bloom filter implementation with n=10000 items, then measure the empirical FPR by querying 100000 non-inserted keys. Plot how FPR changes as n approaches and exceeds the `expectedItems` parameter. Verify it matches the formula `(fill_ratio)^k`.

**Exercise 2 — Extension** (2-4h): Implement a scalable Bloom filter (Almeida et al., 2007) that grows by adding new Bloom filters as capacity is exceeded. The key invariant: each new filter uses a tighter FPR target (multiply by r < 1 each time) so that the total FPR remains bounded. Compare memory usage and lookup performance against a single pre-sized Bloom filter at the same FPR.

**Exercise 3 — From Scratch** (4-8h): Implement a Blocked Bloom filter (Putze et al., 2007) where the bit array is divided into 512-bit blocks and each key's k bits are confined to a single block (selected by one hash). This design fits the entire lookup into a single cache line, reducing lookup latency from ~7 cache misses to 1. Measure the speedup versus the standard implementation at n=10^6 and n=10^7.

**Exercise 4 — Production Scenario** (8-15h): Integrate a Bloom filter into a simple key-value store: implement an in-memory SSTable (a sorted array of key-value pairs) with a companion Bloom filter. Add a multi-level structure (like LevelDB) where each level has its own SSTable and Bloom filter. Measure the read amplification (average disk reads per `Get`) with and without Bloom filters at 10%, 50%, and 90% negative query rates.

## Further Reading

### Foundational Papers
- Bloom, B. H. (1970). "Space/Time Trade-offs in Hash Coding with Allowable Errors." *Communications of the ACM*, 13(7), 422–426. The original 1-page paper that introduced the filter.
- Fan, B., Andersen, D. G., Kaminsky, M., & Mitzenmacher, M. D. (2014). "Cuckoo Filter: Practically Better Than Bloom." *CoNEXT '14*. Introduces the cuckoo filter with detailed FPR and space analysis.
- Graf, T. M., & Lemire, D. (2020). "Xor Filters: Faster and Smaller Than Bloom and Cuckoo Filters." *Journal of Experimental Algorithmics*, 25. Describes the XOR filter construction and proves near-optimal space.
- Kirsch, A., & Mitzenmacher, M. (2006). "Less Hashing, Same Performance: Building a Better Bloom Filter." *ESA 2006*. Introduces the double-hashing approximation.

### Books
- Mitzenmacher, M., & Upfal, E. (2017). *Probability and Computing* (2nd ed.). Cambridge University Press. Chapter 5 covers Bloom filters with full probability proofs.
- Flajolet, P., & Sedgewick, R. (2009). *Analytic Combinatorics*. Appendix covers hash-based probabilistic structures.

### Production Code to Read
- `facebook/rocksdb/util/bloom_impl.h` (https://github.com/facebook/rocksdb) — RocksDB's optimized Bloom filter with SIMD probing. The comment explaining the cache-locality optimization is particularly instructive.
- `seiflotfy/cuckoofilter` (https://github.com/seiflotfy/cuckoofilter) — Clean Go implementation; study bucket packing and the alt-index design.
- `FastFilter/xorfilter` (https://github.com/FastFilter/xorfilter) — Reference Go implementation from the paper authors.

### Conference Talks
- Mitzenmacher, M. (Usenix ATC 2018): "A Model for Learned Bloom Filters and Optimizing by Sandwiching" — introduces machine learning as a pre-filter to reduce the Bloom filter's required size.
- Lemire, D. (HYTRADBOI 2022): "Faster Filters" — benchmarks XOR vs Cuckoo vs Bloom with SIMD optimization details.
