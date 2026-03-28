<!-- difficulty: intermediate-advanced -->
<!-- category: data-structures -->
<!-- languages: [rust] -->
<!-- concepts: [bloom-filter, hashing, probabilistic-data-structures, generics, serialization] -->
<!-- estimated_time: 4-6 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [hash-trait, bitwise-operations, generics-trait-bounds, serde-basics] -->

# Challenge 03: Bloom Filter with Dynamic Resizing

## Languages

Rust (stable, latest edition)

## Prerequisites

- Comfortable with Rust generics and trait bounds (`Hash`, `Sized`)
- Understanding of bitwise operations and bit manipulation
- Familiarity with hashing concepts (hash functions, collision, distribution)
- Basic knowledge of `serde` for serialization/deserialization

## Learning Objectives

- **Implement** a probabilistic data structure with configurable false positive rates
- **Analyze** the trade-offs between memory usage, number of hash functions, and false positive probability
- **Apply** multiple hashing strategies using Rust's `Hash` trait and custom hashers
- **Design** a resizing mechanism that preserves existing membership guarantees
- **Differentiate** between standard and counting Bloom filter semantics

## The Challenge

Build a generic Bloom filter that supports automatic resizing when the estimated false positive rate exceeds a user-defined threshold. A Bloom filter is a space-efficient probabilistic set: it can tell you "definitely not in the set" or "probably in the set" but never gives false negatives.

Your implementation must be generic over any type implementing `Hash`. Support at least two hash function families (the double-hashing technique using SipHash and FNV is sufficient). When the filter becomes too full, it must resize by creating a larger bit array and re-inserting all tracked elements -- which means you need a strategy for element replay.

Additionally, implement a counting Bloom filter variant that supports element removal by replacing single bits with small counters. Provide set operations (union, intersection) between compatible filters, and full serialization/deserialization so filters can be persisted and shared.

## Requirements

1. Implement `BloomFilter<T: Hash>` with configurable expected capacity and desired false positive rate
2. Use the double-hashing technique: derive `k` hash values from two independent base hashes
3. Calculate optimal bit array size (`m`) and number of hash functions (`k`) from capacity and FP rate
4. Track insertion count and estimate current false positive probability
5. Auto-resize (rehash into a larger filter) when estimated FP rate exceeds threshold
6. Implement a `CountingBloomFilter<T: Hash>` variant supporting `remove()` with 4-bit counters
7. Implement `union()` and `intersection()` for two filters with identical parameters
8. Serialize and deserialize the filter using `serde` (JSON and binary via `bincode`)
9. Provide `contains()`, `insert()`, `estimated_fp_rate()`, `len()`, and `clear()`
10. Write unit tests covering edge cases: empty filter, at-capacity, post-resize correctness

## Hints

<details>
<summary>Hint 1: Optimal parameters formula</summary>

Given expected number of elements `n` and desired false positive rate `p`:

```rust
let m = (-(n as f64) * p.ln() / (2.0_f64.ln().powi(2))).ceil() as usize;
let k = ((m as f64 / n as f64) * 2.0_f64.ln()).ceil() as usize;
```

</details>

<details>
<summary>Hint 2: Double hashing technique</summary>

Instead of `k` independent hash functions, compute two base hashes and combine:

```rust
use std::hash::{BuildHasher, Hasher};
use std::collections::hash_map::RandomState;

fn hash_pair<T: Hash>(item: &T) -> (u64, u64) {
    let hasher1 = RandomState::new();
    let hasher2 = RandomState::new();
    let h1 = {
        let mut h = hasher1.build_hasher();
        item.hash(&mut h);
        h.finish()
    };
    let h2 = {
        let mut h = hasher2.build_hasher();
        item.hash(&mut h);
        h.finish()
    };
    (h1, h2)
}

// For the i-th hash: h1.wrapping_add(i as u64 * h2) % bit_count
```

</details>

<details>
<summary>Hint 3: Resizing strategy</summary>

Bloom filters cannot be resized in-place because you cannot extract individual elements from the bit array. Two approaches:

1. **Shadow list**: keep a `Vec<T>` of all inserted elements (uses more memory, simple to implement). On resize, create a new larger filter and re-insert everything.
2. **Scalable Bloom Filter**: chain multiple filters of increasing size. A lookup checks all filters. This avoids re-insertion but increases lookup cost.

For this challenge, the shadow list approach is sufficient.

</details>

<details>
<summary>Hint 4: Counting filter counters</summary>

Use a `Vec<u8>` where each byte stores two 4-bit counters (nibbles). This halves memory compared to one counter per byte:

```rust
fn get_counter(&self, index: usize) -> u8 {
    let byte = self.counters[index / 2];
    if index % 2 == 0 { byte & 0x0F } else { byte >> 4 }
}

fn set_counter(&mut self, index: usize, value: u8) {
    let byte = &mut self.counters[index / 2];
    if index % 2 == 0 {
        *byte = (*byte & 0xF0) | (value & 0x0F);
    } else {
        *byte = (*byte & 0x0F) | ((value & 0x0F) << 4);
    }
}
```

</details>

## Acceptance Criteria

- [ ] `BloomFilter<T>` is generic over any `T: Hash`
- [ ] Optimal `m` and `k` are computed from capacity and desired FP rate
- [ ] `insert()` and `contains()` work correctly with zero false negatives
- [ ] `estimated_fp_rate()` returns a value that grows as elements are inserted
- [ ] Filter auto-resizes when estimated FP rate exceeds the configured threshold
- [ ] `CountingBloomFilter<T>` supports `remove()` without false negatives for remaining elements
- [ ] `union()` and `intersection()` produce correct results for compatible filters
- [ ] Serialization round-trip preserves all membership information
- [ ] False positive rate measured over 100k random lookups is within 2x of theoretical prediction
- [ ] All tests pass with `cargo test`

## Research Resources

- [Bloom Filters by Example](https://llimllib.github.io/bloomfilter-tutorial/) -- interactive tutorial with visualizations
- [Network Applications of Bloom Filters: A Survey (Broder & Mitzenmacher)](https://www.eecs.harvard.edu/~michaelm/postscripts/im2005b.pdf) -- the definitive survey paper
- [Scalable Bloom Filters (Almeida et al.)](https://gsd.di.uminho.pt/members/cbm/ps/dbloom.pdf) -- the resizing approach using filter chains
- [Rust `bitvec` crate docs](https://docs.rs/bitvec/latest/bitvec/) -- efficient bit array manipulation in Rust
- [Rust `serde` book](https://serde.rs/) -- serialization framework documentation
- [Wikipedia: Counting Bloom Filter](https://en.wikipedia.org/wiki/Counting_Bloom_filter) -- theory and counter overflow analysis
