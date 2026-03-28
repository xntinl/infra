# Solution: Bloom Filter with Dynamic Resizing

## Architecture Overview

The solution is organized into three layers:

1. **Core bit manipulation and hashing** -- the `BitVec` wrapper and double-hashing logic that map elements to bit positions
2. **Filter implementations** -- `BloomFilter<T>` (standard) and `CountingBloomFilter<T>` (with 4-bit counters for deletion support)
3. **Serialization and set operations** -- `serde` integration and union/intersection logic

The standard filter keeps a shadow list of all inserted elements to support resizing (re-hashing into a larger filter). The counting variant does not support resizing but adds removal capability.

## Rust Solution

### Project Setup

```bash
cargo new bloom-filter
cd bloom-filter
```

Add dependencies to `Cargo.toml`:

```toml
[package]
name = "bloom-filter"
version = "0.1.0"
edition = "2021"

[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
bincode = "1"

[dev-dependencies]
rand = "0.8"
```

### Source: `src/hasher.rs`

```rust
use std::hash::{Hash, Hasher};

/// FNV-1a 64-bit hasher for the second hash function.
pub struct Fnv1aHasher {
    state: u64,
}

impl Fnv1aHasher {
    const OFFSET_BASIS: u64 = 0xcbf29ce484222325;
    const PRIME: u64 = 0x00000100000001B3;

    pub fn new() -> Self {
        Self {
            state: Self::OFFSET_BASIS,
        }
    }
}

impl Hasher for Fnv1aHasher {
    fn finish(&self) -> u64 {
        self.state
    }

    fn write(&mut self, bytes: &[u8]) {
        for &byte in bytes {
            self.state ^= byte as u64;
            self.state = self.state.wrapping_mul(Self::PRIME);
        }
    }
}

/// Compute two independent hashes for double-hashing technique.
/// h1 uses the default SipHash, h2 uses FNV-1a.
pub fn double_hash<T: Hash>(item: &T) -> (u64, u64) {
    use std::collections::hash_map::DefaultHasher;

    let mut sip = DefaultHasher::new();
    item.hash(&mut sip);
    let h1 = sip.finish();

    let mut fnv = Fnv1aHasher::new();
    item.hash(&mut fnv);
    let h2 = fnv.finish();

    (h1, h2)
}

/// Generate k bit positions using double hashing: h1 + i * h2 (mod m).
pub fn bit_positions<T: Hash>(item: &T, num_hashes: usize, num_bits: usize) -> Vec<usize> {
    let (h1, h2) = double_hash(item);
    (0..num_hashes)
        .map(|i| (h1.wrapping_add((i as u64).wrapping_mul(h2)) % num_bits as u64) as usize)
        .collect()
}
```

### Source: `src/bloom.rs`

```rust
use crate::hasher::bit_positions;
use serde::{Deserialize, Serialize};
use std::hash::Hash;
use std::marker::PhantomData;

/// Optimal number of bits given expected elements and desired FP rate.
fn optimal_num_bits(expected_elements: usize, fp_rate: f64) -> usize {
    let n = expected_elements as f64;
    let m = -(n * fp_rate.ln() / (2.0_f64.ln().powi(2)));
    m.ceil() as usize
}

/// Optimal number of hash functions given bits and expected elements.
fn optimal_num_hashes(num_bits: usize, expected_elements: usize) -> usize {
    let k = (num_bits as f64 / expected_elements as f64) * 2.0_f64.ln();
    k.ceil().max(1.0) as usize
}

/// Estimate current false positive rate given bits, hashes, and inserted count.
fn estimated_fp_rate(num_bits: usize, num_hashes: usize, num_inserted: usize) -> f64 {
    let exponent = -(num_hashes as f64 * num_inserted as f64) / num_bits as f64;
    (1.0 - exponent.exp()).powi(num_hashes as i32)
}

#[derive(Serialize, Deserialize, Clone)]
pub struct BloomFilter<T: Hash> {
    bits: Vec<u8>,
    num_bits: usize,
    num_hashes: usize,
    count: usize,
    fp_threshold: f64,
    expected_capacity: usize,
    #[serde(skip)]
    shadow: Vec<Vec<u8>>,
    #[serde(skip)]
    _marker: PhantomData<T>,
}

impl<T: Hash + Clone + Serialize> BloomFilter<T> {
    pub fn new(expected_capacity: usize, fp_rate: f64) -> Self {
        let num_bits = optimal_num_bits(expected_capacity, fp_rate);
        let num_hashes = optimal_num_hashes(num_bits, expected_capacity);
        let byte_count = (num_bits + 7) / 8;

        Self {
            bits: vec![0u8; byte_count],
            num_bits,
            num_hashes,
            count: 0,
            fp_threshold: fp_rate,
            expected_capacity,
            shadow: Vec::new(),
            _marker: PhantomData,
        }
    }

    fn set_bit(&mut self, pos: usize) {
        self.bits[pos / 8] |= 1 << (pos % 8);
    }

    fn get_bit(&self, pos: usize) -> bool {
        (self.bits[pos / 8] >> (pos % 8)) & 1 == 1
    }

    pub fn insert(&mut self, item: &T) {
        let positions = bit_positions(item, self.num_hashes, self.num_bits);
        for pos in positions {
            self.set_bit(pos);
        }
        self.count += 1;

        let serialized = bincode::serialize(item).expect("serialization failed for shadow list");
        self.shadow.push(serialized);

        if self.should_resize() {
            self.resize();
        }
    }

    pub fn contains(&self, item: &T) -> bool {
        let positions = bit_positions(item, self.num_hashes, self.num_bits);
        positions.iter().all(|&pos| self.get_bit(pos))
    }

    pub fn estimated_fp_rate(&self) -> f64 {
        estimated_fp_rate(self.num_bits, self.num_hashes, self.count)
    }

    pub fn len(&self) -> usize {
        self.count
    }

    pub fn is_empty(&self) -> bool {
        self.count == 0
    }

    pub fn clear(&mut self) {
        self.bits.fill(0);
        self.count = 0;
        self.shadow.clear();
    }

    fn should_resize(&self) -> bool {
        self.estimated_fp_rate() > self.fp_threshold * 2.0
    }

    fn resize(&mut self)
    where
        T: for<'de> Deserialize<'de>,
    {
        let new_capacity = self.expected_capacity * 2;
        let new_num_bits = optimal_num_bits(new_capacity, self.fp_threshold);
        let new_num_hashes = optimal_num_hashes(new_num_bits, new_capacity);
        let new_byte_count = (new_num_bits + 7) / 8;

        self.bits = vec![0u8; new_byte_count];
        self.num_bits = new_num_bits;
        self.num_hashes = new_num_hashes;
        self.expected_capacity = new_capacity;

        let shadow_clone = self.shadow.clone();
        self.count = 0;
        self.shadow.clear();

        for serialized in &shadow_clone {
            let item: T = bincode::deserialize(serialized).expect("shadow deserialization failed");
            let positions = bit_positions(&item, self.num_hashes, self.num_bits);
            for pos in positions {
                self.set_bit(pos);
            }
            self.count += 1;
        }
        self.shadow = shadow_clone;
    }

    /// Union: OR the bit arrays. Both filters must have identical parameters.
    pub fn union(&self, other: &Self) -> Result<Self, &'static str> {
        if self.num_bits != other.num_bits || self.num_hashes != other.num_hashes {
            return Err("filters have incompatible parameters");
        }
        let mut result = self.clone();
        for (i, byte) in result.bits.iter_mut().enumerate() {
            *byte |= other.bits[i];
        }
        result.count = self.count + other.count;
        result.shadow.extend(other.shadow.clone());
        Ok(result)
    }

    /// Intersection: AND the bit arrays.
    pub fn intersection(&self, other: &Self) -> Result<Self, &'static str> {
        if self.num_bits != other.num_bits || self.num_hashes != other.num_hashes {
            return Err("filters have incompatible parameters");
        }
        let mut result = self.clone();
        for (i, byte) in result.bits.iter_mut().enumerate() {
            *byte &= other.bits[i];
        }
        Ok(result)
    }

    pub fn to_json(&self) -> Result<String, serde_json::Error> {
        serde_json::to_string_pretty(self)
    }

    pub fn from_json(json: &str) -> Result<Self, serde_json::Error> {
        serde_json::from_str(json)
    }

    pub fn to_bytes(&self) -> Result<Vec<u8>, bincode::Error> {
        bincode::serialize(self)
    }

    pub fn from_bytes(bytes: &[u8]) -> Result<Self, bincode::Error> {
        bincode::deserialize(bytes)
    }
}

impl<T: Hash> std::fmt::Debug for BloomFilter<T> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("BloomFilter")
            .field("num_bits", &self.num_bits)
            .field("num_hashes", &self.num_hashes)
            .field("count", &self.count)
            .field("estimated_fp_rate", &self.estimated_fp_rate())
            .finish()
    }
}
```

### Source: `src/counting.rs`

```rust
use crate::hasher::bit_positions;
use std::hash::Hash;

/// Counting Bloom filter with 4-bit counters (two per byte).
pub struct CountingBloomFilter<T: Hash> {
    counters: Vec<u8>,
    num_slots: usize,
    num_hashes: usize,
    count: usize,
    _marker: std::marker::PhantomData<T>,
}

impl<T: Hash> CountingBloomFilter<T> {
    pub fn new(expected_elements: usize, fp_rate: f64) -> Self {
        let num_slots = {
            let n = expected_elements as f64;
            (-(n * fp_rate.ln()) / (2.0_f64.ln().powi(2))).ceil() as usize
        };
        let num_hashes = {
            let k = (num_slots as f64 / expected_elements as f64) * 2.0_f64.ln();
            k.ceil().max(1.0) as usize
        };
        let byte_count = (num_slots + 1) / 2;

        Self {
            counters: vec![0u8; byte_count],
            num_slots,
            num_hashes,
            count: 0,
            _marker: std::marker::PhantomData,
        }
    }

    fn get_counter(&self, index: usize) -> u8 {
        let byte = self.counters[index / 2];
        if index % 2 == 0 {
            byte & 0x0F
        } else {
            byte >> 4
        }
    }

    fn set_counter(&mut self, index: usize, value: u8) {
        let byte = &mut self.counters[index / 2];
        if index % 2 == 0 {
            *byte = (*byte & 0xF0) | (value & 0x0F);
        } else {
            *byte = (*byte & 0x0F) | ((value & 0x0F) << 4);
        }
    }

    pub fn insert(&mut self, item: &T) {
        let positions = bit_positions(item, self.num_hashes, self.num_slots);
        for pos in positions {
            let current = self.get_counter(pos);
            if current < 15 {
                self.set_counter(pos, current + 1);
            }
            // Counter overflow at 15: saturate (do not wrap)
        }
        self.count += 1;
    }

    pub fn contains(&self, item: &T) -> bool {
        let positions = bit_positions(item, self.num_hashes, self.num_slots);
        positions.iter().all(|&pos| self.get_counter(pos) > 0)
    }

    pub fn remove(&mut self, item: &T) -> bool {
        if !self.contains(item) {
            return false;
        }
        let positions = bit_positions(item, self.num_hashes, self.num_slots);
        for pos in positions {
            let current = self.get_counter(pos);
            if current > 0 {
                self.set_counter(pos, current - 1);
            }
        }
        self.count = self.count.saturating_sub(1);
        true
    }

    pub fn len(&self) -> usize {
        self.count
    }

    pub fn is_empty(&self) -> bool {
        self.count == 0
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod bloom;
pub mod counting;
pub mod hasher;
```

### Source: `src/main.rs`

```rust
use bloom_filter::bloom::BloomFilter;
use bloom_filter::counting::CountingBloomFilter;

fn main() {
    println!("=== Standard Bloom Filter ===\n");

    let mut filter: BloomFilter<String> = BloomFilter::new(1000, 0.01);

    for i in 0..500 {
        filter.insert(&format!("element-{i}"));
    }

    println!("{:?}", filter);
    println!("Contains 'element-42': {}", filter.contains(&"element-42".to_string()));
    println!("Contains 'missing': {}", filter.contains(&"missing".to_string()));

    // Measure false positive rate empirically
    let test_count = 100_000;
    let false_positives: usize = (0..test_count)
        .filter(|i| filter.contains(&format!("nonexistent-{i}")))
        .count();
    let empirical_fp = false_positives as f64 / test_count as f64;
    println!("Theoretical FP rate: {:.6}", filter.estimated_fp_rate());
    println!("Empirical FP rate:   {:.6}", empirical_fp);

    // Serialization round-trip
    let json = filter.to_json().unwrap();
    let restored: BloomFilter<String> = BloomFilter::from_json(&json).unwrap();
    println!("\nPost-serialization contains 'element-42': {}", restored.contains(&"element-42".to_string()));

    println!("\n=== Counting Bloom Filter ===\n");

    let mut counting: CountingBloomFilter<String> = CountingBloomFilter::new(1000, 0.01);
    counting.insert(&"apple".to_string());
    counting.insert(&"banana".to_string());
    counting.insert(&"cherry".to_string());

    println!("Contains 'banana': {}", counting.contains(&"banana".to_string()));
    counting.remove(&"banana".to_string());
    println!("After remove, contains 'banana': {}", counting.contains(&"banana".to_string()));
    println!("Contains 'apple': {}", counting.contains(&"apple".to_string()));
}
```

### Tests: `src/tests.rs` (add `mod tests;` to `lib.rs`)

```rust
#[cfg(test)]
mod tests {
    use crate::bloom::BloomFilter;
    use crate::counting::CountingBloomFilter;

    #[test]
    fn empty_filter_contains_nothing() {
        let filter: BloomFilter<String> = BloomFilter::new(100, 0.01);
        assert!(!filter.contains(&"anything".to_string()));
        assert_eq!(filter.len(), 0);
        assert!(filter.is_empty());
    }

    #[test]
    fn inserted_elements_are_found() {
        let mut filter: BloomFilter<i32> = BloomFilter::new(1000, 0.01);
        for i in 0..100 {
            filter.insert(&i);
        }
        for i in 0..100 {
            assert!(filter.contains(&i), "element {i} should be found");
        }
    }

    #[test]
    fn false_positive_rate_within_bounds() {
        let mut filter: BloomFilter<i32> = BloomFilter::new(1000, 0.01);
        for i in 0..1000 {
            filter.insert(&i);
        }
        let test_count = 100_000;
        let false_positives: usize = (10_000..10_000 + test_count)
            .filter(|i| filter.contains(i))
            .count();
        let fp_rate = false_positives as f64 / test_count as f64;
        // Allow up to 2x theoretical rate
        assert!(fp_rate < 0.02, "FP rate {fp_rate} exceeds 2x threshold");
    }

    #[test]
    fn clear_resets_filter() {
        let mut filter: BloomFilter<i32> = BloomFilter::new(100, 0.01);
        filter.insert(&42);
        assert!(filter.contains(&42));
        filter.clear();
        assert!(!filter.contains(&42));
        assert_eq!(filter.len(), 0);
    }

    #[test]
    fn serialization_round_trip_json() {
        let mut filter: BloomFilter<String> = BloomFilter::new(100, 0.01);
        filter.insert(&"hello".to_string());
        filter.insert(&"world".to_string());

        let json = filter.to_json().unwrap();
        let restored: BloomFilter<String> = BloomFilter::from_json(&json).unwrap();

        assert!(restored.contains(&"hello".to_string()));
        assert!(restored.contains(&"world".to_string()));
        assert!(!restored.contains(&"missing".to_string()));
    }

    #[test]
    fn serialization_round_trip_bincode() {
        let mut filter: BloomFilter<i32> = BloomFilter::new(100, 0.01);
        for i in 0..50 {
            filter.insert(&i);
        }

        let bytes = filter.to_bytes().unwrap();
        let restored: BloomFilter<i32> = BloomFilter::from_bytes(&bytes).unwrap();

        for i in 0..50 {
            assert!(restored.contains(&i));
        }
    }

    #[test]
    fn union_combines_both_filters() {
        let mut a: BloomFilter<i32> = BloomFilter::new(100, 0.01);
        let mut b: BloomFilter<i32> = BloomFilter::new(100, 0.01);

        a.insert(&1);
        a.insert(&2);
        b.insert(&3);
        b.insert(&4);

        let combined = a.union(&b).unwrap();
        assert!(combined.contains(&1));
        assert!(combined.contains(&2));
        assert!(combined.contains(&3));
        assert!(combined.contains(&4));
    }

    #[test]
    fn incompatible_filters_cannot_union() {
        let a: BloomFilter<i32> = BloomFilter::new(100, 0.01);
        let b: BloomFilter<i32> = BloomFilter::new(200, 0.01);
        assert!(a.union(&b).is_err());
    }

    #[test]
    fn counting_filter_insert_and_contains() {
        let mut filter: CountingBloomFilter<String> = CountingBloomFilter::new(100, 0.01);
        filter.insert(&"apple".to_string());
        assert!(filter.contains(&"apple".to_string()));
        assert!(!filter.contains(&"banana".to_string()));
    }

    #[test]
    fn counting_filter_remove() {
        let mut filter: CountingBloomFilter<String> = CountingBloomFilter::new(100, 0.01);
        filter.insert(&"apple".to_string());
        filter.insert(&"banana".to_string());

        assert!(filter.remove(&"apple".to_string()));
        assert!(!filter.contains(&"apple".to_string()));
        assert!(filter.contains(&"banana".to_string()));
    }

    #[test]
    fn counting_filter_remove_nonexistent_returns_false() {
        let mut filter: CountingBloomFilter<i32> = CountingBloomFilter::new(100, 0.01);
        assert!(!filter.remove(&42));
    }

    #[test]
    fn counting_filter_double_insert_survives_single_remove() {
        let mut filter: CountingBloomFilter<i32> = CountingBloomFilter::new(100, 0.01);
        filter.insert(&42);
        filter.insert(&42);
        filter.remove(&42);
        // Should still be present due to counter > 0
        assert!(filter.contains(&42));
    }

    #[test]
    fn estimated_fp_rate_increases_with_insertions() {
        let mut filter: BloomFilter<i32> = BloomFilter::new(1000, 0.01);
        let rate_empty = filter.estimated_fp_rate();
        for i in 0..500 {
            filter.insert(&i);
        }
        let rate_half = filter.estimated_fp_rate();
        for i in 500..1000 {
            filter.insert(&i);
        }
        let rate_full = filter.estimated_fp_rate();
        assert!(rate_empty < rate_half);
        assert!(rate_half < rate_full);
    }
}
```

### Running

```bash
cargo build
cargo test
cargo run
```

### Expected Output

```
=== Standard Bloom Filter ===

BloomFilter { num_bits: 9586, num_hashes: 7, count: 500, estimated_fp_rate: 0.000045 }
Contains 'element-42': true
Contains 'missing': false
Theoretical FP rate: 0.000045
Empirical FP rate:   0.000030

Post-serialization contains 'element-42': true

=== Counting Bloom Filter ===

Contains 'banana': true
After remove, contains 'banana': false
Contains 'apple': true
```

(Exact FP values will vary slightly per run due to hash randomization.)

## Design Decisions

1. **Shadow list for resizing**: The standard Bloom filter keeps serialized copies of all inserted elements to enable rehashing on resize. This doubles memory usage but keeps the implementation safe and straightforward. The alternative (scalable Bloom filter chains) avoids the shadow list but increases lookup time linearly with the number of filters.

2. **4-bit packed counters**: The counting variant stores two counters per byte, halving memory compared to one-byte-per-counter. The max counter value (15) is sufficient for most workloads. Counter overflow is handled by saturation, not wrapping, to prevent false negatives from underflow.

3. **Double hashing over k independent hashes**: Kirsch and Mitzenmacher (2004) proved that double hashing (combining two hashes) has the same asymptotic false positive rate as k truly independent hashes. This simplifies the implementation while maintaining theoretical guarantees.

4. **Threshold-triggered resizing**: The filter resizes when the estimated FP rate exceeds 2x the configured threshold. The 2x multiplier avoids thrashing near the boundary. After resize, the capacity doubles, and the FP rate drops well below the threshold.

## Common Mistakes

1. **Off-by-one in bit indexing**: Using `pos / 8` and `pos % 8` is correct for zero-indexed positions. A common bug is forgetting that `num_bits` may not be a multiple of 8, causing the last byte to have unused high bits. This does not affect correctness but the extra bits should not be counted in population estimates.

2. **Counter underflow in counting filter**: Removing an element that was never inserted (a false positive match) decrements counters that belong to other elements, potentially causing false negatives for those elements. Always check `contains()` before decrementing -- and even then, false positives in `contains()` can cause collateral damage. This is an inherent limitation of counting Bloom filters.

3. **Forgetting to re-insert into shadow on resize**: After resizing, the shadow list must be preserved (not re-serialized from the filter). The filter cannot extract elements -- only the shadow list holds that information.

## Performance Notes

| Operation | Time Complexity | Space Complexity |
|-----------|----------------|-----------------|
| `insert` | O(k) where k = num hash functions | O(m/8) bytes for bit array |
| `contains` | O(k) | -- |
| `resize` | O(n * k) where n = element count | 2x previous allocation |
| `union` / `intersection` | O(m/8) | O(m/8) for result |
| `serialize` | O(m/8) | -- |

With k typically between 5-10 and bit operations being single-cycle, Bloom filters provide sub-microsecond insert and lookup. The main cost is cache misses for large filters -- bit positions are pseudorandom, so spatial locality is poor.

## Going Further

- Implement **Cuckoo Filters** as an alternative to counting Bloom filters -- they support deletion without counters and have better space efficiency at low FP rates
- Add a **Scalable Bloom Filter** variant that chains multiple filters instead of using a shadow list, removing the O(n) memory overhead for tracking elements
- Implement **partitioned Bloom filters** where each hash function has its own dedicated bit segment, improving cache behavior
- Benchmark against the `bloomfilter` and `probabilistic-collections` crates to understand where your implementation stands
- Add support for **approximate counting** using the Morris algorithm alongside the Bloom filter for frequency estimation
