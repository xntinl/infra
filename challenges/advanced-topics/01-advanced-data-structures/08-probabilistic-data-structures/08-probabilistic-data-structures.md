<!--
type: reference
difficulty: advanced
section: [01-advanced-data-structures]
concepts: [hyperloglog, count-min-sketch, t-digest, cardinality-estimation, frequency-estimation, quantile-estimation, streaming-algorithms]
languages: [go, rust]
estimated_reading_time: 70 min
bloom_level: analyze
prerequisites: [probability-expected-value, hash-functions, streaming-algorithms-basics]
papers: [flajolet-2007-hyperloglog, cormode-2005-count-min, dunning-2019-t-digest]
industry_use: [redis-hyperloglog, prometheus-histograms, kafka-streams, apache-datasketches]
language_contrast: low
-->

# Probabilistic Data Structures

> Redis's `PFADD`/`PFCOUNT` (HyperLogLog) can count the distinct visitors to a website with 1.6 KB of memory and 0.81% standard error regardless of whether there are 10 or 10 billion unique visitors — the exact count would require 8 GB for 10 billion 64-bit hashes.

## Mental Model

Probabilistic data structures trade exactness for orders-of-magnitude reductions in space and time. The critical mental model shift from deterministic structures: you are not trying to find the right answer — you are estimating a quantity with a bounded error distribution. The question is not "is this accurate?" but "what is the error bound, and is that acceptable for my use case?"

The three structures in this section each solve a distinct estimation problem over data streams:

**HyperLogLog (HLL)**: Estimates the cardinality (number of distinct elements) in a stream. Error is relative (±0.81% with standard parameters). Space is O(log log U) = ~1.5 KB regardless of stream size. Used for: unique visitor counts, A/B test participant counts, SELECT COUNT(DISTINCT ...) over large datasets.

**Count-Min Sketch (CMS)**: Estimates the frequency of individual elements in a stream. Error is additive (the estimated count is at most exact_count + ε × N where ε is configurable and N is total items). Space is O(1/ε × log(1/δ)) where δ is the failure probability. Used for: heavy hitter detection, DDoS source IP tracking, network traffic analysis.

**T-Digest**: Estimates quantiles (p50, p95, p99) of a continuous distribution over a stream. Error is small near the tails (p0, p100) and bounded near the median. Space is O(compression parameter). Used for: latency percentile tracking, Prometheus histograms, tail latency SLO monitoring.

The unifying principle: all three exploit the fact that the interesting quantities (cardinality, frequency, quantiles) can be estimated using summary statistics that are far smaller than the data itself. The mathematical guarantee is probabilistic: with high probability (1-δ), the estimate is within ε of the true value.

## Core Concepts

### HyperLogLog: Maximum Zero Run as Cardinality Estimator

The foundational observation: if you hash n distinct elements to uniform random bit strings, the probability that at least one hash starts with k zeros is approximately 1 - (1 - 2^(-k))^n ≈ 1 - e^(-n/2^k). Observing k leading zeros is evidence that n ≈ 2^k.

The raw estimator (observe the maximum number of leading zeros across all hashes) has high variance. HyperLogLog reduces variance by splitting the stream into m = 2^b buckets using the first b bits of each hash, maintaining the maximum zero run in each bucket, and taking a harmonic mean of the 2^(max_zeros_i) estimates across all buckets. The standard error is approximately 1.04/sqrt(m).

With m=2^14 = 16384 buckets, each requiring 6 bits (stores values 0-63, enough for 64-bit hashes), the total storage is 16384 × 6 bits / 8 = 12288 bytes ≈ 12 KB. Standard error: 1.04/sqrt(16384) = 0.008125 = 0.8125%.

Redis uses m=2^14 registers of 6 bits = 12 KB per HLL. The implementation includes bias correction terms (from the Flajolet et al. paper) for small and large cardinality ranges where the raw harmonic mean formula is inaccurate.

### Count-Min Sketch: Conservative Update and Point Queries

A Count-Min Sketch is a d×w array of counters, where d is the number of hash functions and w is the width. Each element in the stream is hashed by all d hash functions, and the corresponding d counters are incremented. A frequency estimate for an element is the minimum over all d counters for that element.

Why minimum? Because counters can only be overestimated (hash collisions add false counts). The minimum provides the tightest upper bound. The error bound: with probability 1-δ, the estimate is at most exact_count + ε × N, where w = ceil(e/ε) and d = ceil(ln(1/δ)).

For ε=0.01 (1% error) and δ=0.01 (1% failure probability): w = ceil(e/0.01) ≈ 272, d = ceil(ln(100)) ≈ 5. Total: 272 × 5 = 1360 counters. For 32-bit counters, that is 5.4 KB for 1% error with 99% confidence — regardless of stream size.

**Conservative update**: For point queries (not range queries), replace "increment all d counters" with "increment only counters that are currently at their minimum." This reduces over-counting caused by hash collisions. Conservative update is strictly better for point queries.

### T-Digest: Clustering Quantile Estimation

T-Digest maintains a set of centroids — each centroid is a (mean, count) pair. Incoming values are merged into the nearest centroid if the centroid's count is within a size limit, or create a new centroid otherwise. The size limit for a centroid at quantile q is `δ × q × (1-q) × n / 2`, where δ is the compression parameter and n is the total count seen.

This limit has a key property: near the tails (q≈0 or q≈1), the size limit is very small — centroid counts are at most 1-2 near the extremes. This means tail quantiles (p1, p99) are represented very accurately. Near the median (q≈0.5), the limit is large — the median centroid may represent millions of values with a single (mean, count) pair. This is exactly the right trade-off for latency SLOs: p99 matters, median usually does not.

The accuracy guarantee: near the tails at quantile q, the error in the estimated quantile is O(δ⁻¹ × n⁻¹) — decreasing with both larger n and smaller δ (larger compression). In practice, δ=100 gives acceptable accuracy for p99 with 20-100 centroids.

## Implementation: Go

```go
package main

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"math/bits"
	"sort"
)

// HyperLogLog estimates cardinality with configurable precision.
// precision p: m=2^p registers, standard error ≈ 1.04/sqrt(m).
// For p=14 (m=16384): ~12 KB storage, ~0.81% standard error.
type HyperLogLog struct {
	registers []uint8 // m registers, each 6 bits (stores max leading zeros 0-63)
	m         uint32  // number of registers = 2^p
	p         uint8   // precision (number of bits used for register index)
	alpha     float64 // bias correction constant
}

func NewHyperLogLog(p uint8) *HyperLogLog {
	if p < 4 || p > 18 {
		p = 14 // default: 16384 registers, 12 KB, 0.81% error
	}
	m := uint32(1) << p
	alpha := computeAlpha(m)
	return &HyperLogLog{
		registers: make([]uint8, m),
		m:         m,
		p:         p,
		alpha:     alpha,
	}
}

func computeAlpha(m uint32) float64 {
	switch m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	default:
		// Asymptotic value: 0.7213 / (1 + 1.079/m)
		return 0.7213 / (1 + 1.079/float64(m))
	}
}

func (hll *HyperLogLog) hash(item []byte) uint64 {
	h := fnv.New64a()
	h.Write(item)
	return h.Sum64()
}

// Add adds an item to the HyperLogLog.
func (hll *HyperLogLog) Add(item []byte) {
	hashVal := hll.hash(item)
	// Register index: the first p bits of the hash
	registerIdx := uint32(hashVal >> (64 - uint64(hll.p)))
	// Leading zeros in the remaining 64-p bits (+1 for the position convention)
	remaining := hashVal<<hll.p | (1<<hll.p - 1) // shift out the p index bits
	leadingZeros := uint8(bits.LeadingZeros64(remaining)) + 1
	if leadingZeros > hll.registers[registerIdx] {
		hll.registers[registerIdx] = leadingZeros
	}
}

// Count returns the estimated cardinality.
func (hll *HyperLogLog) Count() uint64 {
	// Compute the raw estimate: alpha_m * m^2 / sum(2^(-M_j))
	var harmonicSum float64
	for _, reg := range hll.registers {
		harmonicSum += math.Pow(2, -float64(reg))
	}
	m := float64(hll.m)
	rawEstimate := hll.alpha * m * m / harmonicSum

	// Small range correction: if estimate is very small, use linear counting
	if rawEstimate <= 2.5*m {
		zeroRegisters := 0
		for _, reg := range hll.registers {
			if reg == 0 {
				zeroRegisters++
			}
		}
		if zeroRegisters > 0 {
			// Linear counting: -m * ln(V/m) where V is the number of zero registers
			return uint64(m * math.Log(m/float64(zeroRegisters)))
		}
	}

	// Large range correction for values near 2^32
	if rawEstimate > (1/30.0)*math.Pow(2, 32) {
		rawEstimate = -math.Pow(2, 32) * math.Log(1-rawEstimate/math.Pow(2, 32))
	}

	return uint64(rawEstimate)
}

// Merge combines two HyperLogLog structures (they must have the same precision).
// This enables distributed cardinality estimation: compute HLL per shard, merge centrally.
func (hll *HyperLogLog) Merge(other *HyperLogLog) error {
	if hll.p != other.p {
		return fmt.Errorf("precision mismatch: %d != %d", hll.p, other.p)
	}
	for i := range hll.registers {
		if other.registers[i] > hll.registers[i] {
			hll.registers[i] = other.registers[i]
		}
	}
	return nil
}

// CountMinSketch estimates element frequencies in a stream.
// d rows (hash functions), w columns. O(d × w) space = O(1/ε × log(1/δ)).
type CountMinSketch struct {
	table  [][]uint32
	d      int // number of hash functions
	w      int // width of each row
	hashes []uint64 // seeds for each hash function
	total  uint64   // total items added
}

func NewCountMinSketch(epsilon, delta float64) *CountMinSketch {
	w := int(math.Ceil(math.E / epsilon))
	d := int(math.Ceil(math.Log(1 / delta)))
	table := make([][]uint32, d)
	hashes := make([]uint64, d)
	for i := range table {
		table[i] = make([]uint32, w)
		// Different hash seeds per row; using FNV with different initial values
		hashes[i] = uint64(i+1) * 0x9e3779b97f4a7c15
	}
	return &CountMinSketch{table: table, d: d, w: w, hashes: hashes}
}

func (cms *CountMinSketch) hashPos(item []byte, seed uint64) int {
	h := fnv.New64a()
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, seed)
	h.Write(b)
	h.Write(item)
	return int(h.Sum64() % uint64(cms.w))
}

// Add increments all d counters for item (standard update).
func (cms *CountMinSketch) Add(item []byte) {
	for i := 0; i < cms.d; i++ {
		pos := cms.hashPos(item, cms.hashes[i])
		cms.table[i][pos]++
	}
	cms.total++
}

// AddConservative uses conservative update: only increment counters at the current minimum.
// This reduces over-counting and improves accuracy for point queries.
func (cms *CountMinSketch) AddConservative(item []byte) {
	positions := make([]int, cms.d)
	minVal := uint32(math.MaxUint32)
	for i := 0; i < cms.d; i++ {
		pos := cms.hashPos(item, cms.hashes[i])
		positions[i] = pos
		if cms.table[i][pos] < minVal {
			minVal = cms.table[i][pos]
		}
	}
	for i := 0; i < cms.d; i++ {
		if cms.table[i][positions[i]] == minVal {
			cms.table[i][positions[i]]++
		}
	}
	cms.total++
}

// Estimate returns the estimated frequency of item (an upper bound on the true frequency).
func (cms *CountMinSketch) Estimate(item []byte) uint32 {
	minCount := uint32(math.MaxUint32)
	for i := 0; i < cms.d; i++ {
		pos := cms.hashPos(item, cms.hashes[i])
		if cms.table[i][pos] < minCount {
			minCount = cms.table[i][pos]
		}
	}
	return minCount
}

// TDigest estimates quantiles of a continuous distribution.
// Centroids near the tails are small (high accuracy); those near the median are large.
type centroid struct {
	mean  float64
	count float64
}

type TDigest struct {
	centroids   []centroid
	compression float64 // δ: smaller = more centroids = more accurate
	totalCount  float64
}

func NewTDigest(compression float64) *TDigest {
	return &TDigest{compression: compression}
}

// Add adds a single value to the T-Digest.
func (td *TDigest) Add(value float64) {
	td.AddWeighted(value, 1.0)
}

// AddWeighted adds a value with a given weight (for pre-aggregated data).
func (td *TDigest) AddWeighted(value, weight float64) {
	td.totalCount += weight

	if len(td.centroids) == 0 {
		td.centroids = append(td.centroids, centroid{mean: value, count: weight})
		return
	}

	// Find the nearest centroid by binary search on sorted means
	idx := sort.Search(len(td.centroids), func(i int) bool {
		return td.centroids[i].mean >= value
	})

	// Try to merge with the nearest centroid if within size budget
	bestIdx := -1
	bestDist := math.MaxFloat64
	for _, candidate := range []int{idx - 1, idx} {
		if candidate < 0 || candidate >= len(td.centroids) {
			continue
		}
		dist := math.Abs(td.centroids[candidate].mean - value)
		if dist < bestDist {
			// Compute this centroid's quantile rank to check size budget
			var cumulativeBefore float64
			for j := 0; j < candidate; j++ {
				cumulativeBefore += td.centroids[j].count
			}
			q := (cumulativeBefore + td.centroids[candidate].count/2) / td.totalCount
			// The size limit at quantile q: δ * q * (1-q)
			limit := td.compression * q * (1 - q)
			if td.centroids[candidate].count+weight <= limit {
				bestDist = dist
				bestIdx = candidate
			}
		}
	}

	if bestIdx >= 0 {
		c := &td.centroids[bestIdx]
		// Update the centroid's mean (weighted average)
		c.mean = (c.mean*c.count + value*weight) / (c.count + weight)
		c.count += weight
	} else {
		// Insert a new centroid in sorted position
		newC := centroid{mean: value, count: weight}
		td.centroids = append(td.centroids, newC)
		// Insertion sort to maintain sorted order (efficient for one insertion)
		for i := len(td.centroids) - 1; i > 0 && td.centroids[i].mean < td.centroids[i-1].mean; i-- {
			td.centroids[i], td.centroids[i-1] = td.centroids[i-1], td.centroids[i]
		}
	}

	// Periodically compress if centroid count grows too large
	if len(td.centroids) > int(5*td.compression) {
		td.compress()
	}
}

// compress merges adjacent centroids that are within budget.
func (td *TDigest) compress() {
	if len(td.centroids) < 2 {
		return
	}
	merged := []centroid{td.centroids[0]}
	for i := 1; i < len(td.centroids); i++ {
		c := &merged[len(merged)-1]
		var cumBefore float64
		for j := 0; j < len(merged)-1; j++ {
			cumBefore += merged[j].count
		}
		q := (cumBefore + c.count/2) / td.totalCount
		limit := td.compression * q * (1 - q)
		if c.count+td.centroids[i].count <= limit {
			c.mean = (c.mean*c.count + td.centroids[i].mean*td.centroids[i].count) /
				(c.count + td.centroids[i].count)
			c.count += td.centroids[i].count
		} else {
			merged = append(merged, td.centroids[i])
		}
	}
	td.centroids = merged
}

// Quantile returns the estimated value at quantile q (0.0 to 1.0).
func (td *TDigest) Quantile(q float64) float64 {
	if q < 0 || q > 1 || len(td.centroids) == 0 {
		return math.NaN()
	}
	if q == 0 {
		return td.centroids[0].mean
	}
	if q == 1 {
		return td.centroids[len(td.centroids)-1].mean
	}

	targetRank := q * td.totalCount
	var cumulative float64
	for i, c := range td.centroids {
		lower := cumulative
		upper := cumulative + c.count
		if targetRank >= lower && targetRank < upper {
			// Interpolate within this centroid
			if i == 0 {
				return c.mean
			}
			frac := (targetRank - lower) / c.count
			return c.mean + frac*(td.centroids[i].mean-td.centroids[i-1].mean)
		}
		cumulative += c.count
	}
	return td.centroids[len(td.centroids)-1].mean
}

func main() {
	fmt.Println("=== HyperLogLog ===")
	hll := NewHyperLogLog(14) // p=14: 16384 registers, 0.81% error

	// Add 1 million distinct items
	for i := 0; i < 1_000_000; i++ {
		key := []byte(fmt.Sprintf("user-%d", i))
		hll.Add(key)
	}
	estimate := hll.Count()
	fmt.Printf("True count: 1,000,000\n")
	fmt.Printf("HLL estimate: %d\n", estimate)
	fmt.Printf("Error: %.4f%%\n", math.Abs(float64(estimate)-1_000_000)/1_000_000*100)
	fmt.Printf("Storage: %d bytes (%.0f KB)\n", len(hll.registers), float64(len(hll.registers))/1024)

	// Add same items again — cardinality should not change (set semantics)
	for i := 0; i < 1_000_000; i++ {
		key := []byte(fmt.Sprintf("user-%d", i))
		hll.Add(key)
	}
	fmt.Printf("After re-adding same items: %d (should still be ~1M)\n", hll.Count())

	fmt.Println("\n=== Count-Min Sketch ===")
	cms := NewCountMinSketch(0.01, 0.01) // 1% error with 99% confidence
	fmt.Printf("Sketch size: %d × %d = %d counters\n", cms.d, cms.w, cms.d*cms.w)

	// Simulate a heavy hitter: "item-0" appears 50% of the time
	totalItems := 100_000
	for i := 0; i < totalItems; i++ {
		if i%2 == 0 {
			cms.AddConservative([]byte("item-0"))
		} else {
			cms.AddConservative([]byte(fmt.Sprintf("item-%d", i)))
		}
	}

	heavyHitterTrue := totalItems / 2
	heavyHitterEst := cms.Estimate([]byte("item-0"))
	fmt.Printf("item-0 true count: %d\n", heavyHitterTrue)
	fmt.Printf("item-0 estimated: %d\n", heavyHitterEst)
	fmt.Printf("Overcount: %d (should be ≤ %d)\n",
		int(heavyHitterEst)-heavyHitterTrue,
		int(math.Ceil(float64(totalItems)*0.01)))

	// Rare item should have low estimate
	rareItemEst := cms.Estimate([]byte("item-99999"))
	fmt.Printf("item-99999 (appeared once) estimated: %d\n", rareItemEst)

	fmt.Println("\n=== T-Digest ===")
	td := NewTDigest(100) // compression=100: ~100 centroids for typical workloads

	// Simulate latency distribution: mostly 10-50ms, tail up to 1000ms
	// Mix of normal and long-tail values
	for i := 0; i < 100_000; i++ {
		// 99% of requests: 10-50ms (uniform for simplicity)
		td.Add(float64(10 + i%40))
		if i%100 == 0 {
			// 1% are slow: 200-1000ms
			td.Add(float64(200 + i%800))
		}
	}

	fmt.Printf("Total observations: %.0f\n", td.totalCount)
	fmt.Printf("Centroid count: %d\n", len(td.centroids))
	fmt.Printf("p50 (median): %.1f ms\n", td.Quantile(0.50))
	fmt.Printf("p90: %.1f ms\n", td.Quantile(0.90))
	fmt.Printf("p95: %.1f ms\n", td.Quantile(0.95))
	fmt.Printf("p99: %.1f ms\n", td.Quantile(0.99))
	fmt.Printf("p999: %.1f ms\n", td.Quantile(0.999))
}
```

### Go-specific considerations

The `hash/fnv` package is used throughout. For production, `xxhash` or `SipHash-1-3` provides better distribution and is 2-3x faster than FNV-64a for short strings. The hash function choice affects the error rate: a poorly distributed hash causes more collisions in Count-Min Sketch and correlation in HyperLogLog registers.

For concurrent access in Go: all three structures require careful synchronization. For HyperLogLog, the registers are updated independently per item — a fine-grained approach uses one `sync.Mutex` per register block (e.g., 64 registers per lock). For Count-Min Sketch, row-level mutexes work. For T-Digest, the centroid list is the shared state and requires a single mutex for consistency.

The HyperLogLog Merge function is the key distributed primitive: compute HLLs in parallel across shards, then merge to get the global cardinality. This is exactly how Redis Cluster handles `PFCOUNT` across multiple nodes.

## Implementation: Rust

```rust
use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};

// HyperLogLog with configurable precision.
pub struct HyperLogLog {
    registers: Vec<u8>,
    m: usize,
    p: u8,
    alpha: f64,
}

impl HyperLogLog {
    pub fn new(p: u8) -> Self {
        let p = p.clamp(4, 18);
        let m = 1usize << p;
        let alpha = match m {
            16 => 0.673,
            32 => 0.697,
            64 => 0.709,
            _ => 0.7213 / (1.0 + 1.079 / m as f64),
        };
        HyperLogLog { registers: vec![0u8; m], m, p, alpha }
    }

    fn hash_item(&self, item: &[u8]) -> u64 {
        let mut h = DefaultHasher::new();
        item.hash(&mut h);
        h.finish()
    }

    pub fn add(&mut self, item: &[u8]) {
        let hash_val = self.hash_item(item);
        let register_idx = (hash_val >> (64 - self.p as u64)) as usize;
        // Count leading zeros of the remaining bits
        let remaining = hash_val.wrapping_shl(self.p as u32) | ((1u64 << self.p) - 1);
        let leading_zeros = (remaining.leading_zeros() + 1) as u8;
        if leading_zeros > self.registers[register_idx] {
            self.registers[register_idx] = leading_zeros;
        }
    }

    pub fn count(&self) -> u64 {
        let m = self.m as f64;
        let harmonic_sum: f64 = self.registers
            .iter()
            .map(|&r| 2.0_f64.powi(-(r as i32)))
            .sum();
        let raw = self.alpha * m * m / harmonic_sum;

        // Small range correction
        if raw <= 2.5 * m {
            let zero_count = self.registers.iter().filter(|&&r| r == 0).count();
            if zero_count > 0 {
                return (m * (m / zero_count as f64).ln()) as u64;
            }
        }

        // Large range correction
        let raw = if raw > (1.0 / 30.0) * 2.0_f64.powi(32) {
            -2.0_f64.powi(32) * (1.0 - raw / 2.0_f64.powi(32)).ln()
        } else {
            raw
        };

        raw as u64
    }

    pub fn merge(&mut self, other: &HyperLogLog) -> Result<(), &'static str> {
        if self.p != other.p {
            return Err("precision mismatch");
        }
        for (a, &b) in self.registers.iter_mut().zip(other.registers.iter()) {
            *a = (*a).max(b);
        }
        Ok(())
    }
}

// Count-Min Sketch with configurable accuracy.
pub struct CountMinSketch {
    table: Vec<Vec<u32>>,
    d: usize,
    w: usize,
    seeds: Vec<u64>,
    total: u64,
}

impl CountMinSketch {
    pub fn new(epsilon: f64, delta: f64) -> Self {
        let w = (std::f64::consts::E / epsilon).ceil() as usize;
        let d = (1.0 / delta).ln().ceil() as usize;
        let seeds: Vec<u64> = (0..d).map(|i| (i as u64 + 1).wrapping_mul(0x9e3779b97f4a7c15)).collect();
        CountMinSketch {
            table: vec![vec![0u32; w]; d],
            d, w, seeds,
            total: 0,
        }
    }

    fn hash_pos(&self, item: &[u8], seed: u64) -> usize {
        let mut h = DefaultHasher::new();
        seed.hash(&mut h);
        item.hash(&mut h);
        (h.finish() as usize) % self.w
    }

    pub fn add_conservative(&mut self, item: &[u8]) {
        let positions: Vec<usize> = (0..self.d)
            .map(|i| self.hash_pos(item, self.seeds[i]))
            .collect();
        let min_val = positions.iter().enumerate()
            .map(|(i, &pos)| self.table[i][pos])
            .min()
            .unwrap_or(0);
        for (i, &pos) in positions.iter().enumerate() {
            if self.table[i][pos] == min_val {
                self.table[i][pos] += 1;
            }
        }
        self.total += 1;
    }

    pub fn estimate(&self, item: &[u8]) -> u32 {
        (0..self.d)
            .map(|i| self.table[i][self.hash_pos(item, self.seeds[i])])
            .min()
            .unwrap_or(0)
    }
}

// T-Digest for quantile estimation.
#[derive(Clone)]
struct Centroid {
    mean: f64,
    count: f64,
}

pub struct TDigest {
    centroids: Vec<Centroid>,
    compression: f64,
    total_count: f64,
}

impl TDigest {
    pub fn new(compression: f64) -> Self {
        TDigest { centroids: vec![], compression, total_count: 0.0 }
    }

    pub fn add(&mut self, value: f64) {
        self.add_weighted(value, 1.0);
    }

    pub fn add_weighted(&mut self, value: f64, weight: f64) {
        self.total_count += weight;

        if self.centroids.is_empty() {
            self.centroids.push(Centroid { mean: value, count: weight });
            return;
        }

        // Find insertion point
        let idx = self.centroids.partition_point(|c| c.mean < value);

        let mut best_idx: Option<usize> = None;
        let mut best_dist = f64::MAX;

        for candidate in [idx.wrapping_sub(1), idx] {
            if candidate >= self.centroids.len() { continue; }
            let dist = (self.centroids[candidate].mean - value).abs();
            if dist < best_dist {
                let cum_before: f64 = self.centroids[..candidate].iter().map(|c| c.count).sum();
                let q = (cum_before + self.centroids[candidate].count / 2.0) / self.total_count;
                let limit = self.compression * q * (1.0 - q);
                if self.centroids[candidate].count + weight <= limit {
                    best_dist = dist;
                    best_idx = Some(candidate);
                }
            }
        }

        match best_idx {
            Some(i) => {
                let c = &mut self.centroids[i];
                c.mean = (c.mean * c.count + value * weight) / (c.count + weight);
                c.count += weight;
            }
            None => {
                self.centroids.insert(idx, Centroid { mean: value, count: weight });
            }
        }

        if self.centroids.len() > (5.0 * self.compression) as usize {
            self.compress();
        }
    }

    fn compress(&mut self) {
        if self.centroids.len() < 2 { return; }
        let mut merged: Vec<Centroid> = vec![self.centroids[0].clone()];
        for c in self.centroids.iter().skip(1) {
            let last = merged.last_mut().unwrap();
            let cum_before: f64 = merged[..merged.len()-1].iter().map(|x| x.count).sum();
            let q = (cum_before + last.count / 2.0) / self.total_count;
            let limit = self.compression * q * (1.0 - q);
            if last.count + c.count <= limit {
                last.mean = (last.mean * last.count + c.mean * c.count) / (last.count + c.count);
                last.count += c.count;
            } else {
                merged.push(c.clone());
            }
        }
        self.centroids = merged;
    }

    pub fn quantile(&self, q: f64) -> Option<f64> {
        if self.centroids.is_empty() || !(0.0..=1.0).contains(&q) { return None; }
        if q == 0.0 { return Some(self.centroids[0].mean); }
        if q == 1.0 { return Some(self.centroids.last().unwrap().mean); }

        let target = q * self.total_count;
        let mut cumulative = 0.0f64;
        for (i, c) in self.centroids.iter().enumerate() {
            if target < cumulative + c.count {
                if i == 0 { return Some(c.mean); }
                let frac = (target - cumulative) / c.count;
                return Some(c.mean + frac * (c.mean - self.centroids[i - 1].mean));
            }
            cumulative += c.count;
        }
        self.centroids.last().map(|c| c.mean)
    }
}

fn main() {
    println!("=== HyperLogLog ===");
    let mut hll = HyperLogLog::new(14);
    for i in 0u64..100_000 {
        hll.add(&i.to_le_bytes());
    }
    let est = hll.count();
    let error = ((est as i64 - 100_000i64).abs() as f64) / 100_000.0 * 100.0;
    println!("True: 100,000 | Estimated: {} | Error: {:.3}%", est, error);
    println!("Storage: {} bytes", hll.registers.len());

    println!("\n=== Count-Min Sketch ===");
    let mut cms = CountMinSketch::new(0.01, 0.01);
    println!("{}×{} = {} counters ({} bytes)", cms.d, cms.w, cms.d * cms.w, cms.d * cms.w * 4);
    for i in 0u64..10_000 {
        cms.add_conservative(&i.to_le_bytes());
        if i % 10 == 0 {
            cms.add_conservative(&0u64.to_le_bytes()); // item 0 appears 10% of the time
        }
    }
    let est_heavy = cms.estimate(&0u64.to_le_bytes());
    println!("item-0 (appeared ~1100 times), estimated: {}", est_heavy);

    println!("\n=== T-Digest ===");
    let mut td = TDigest::new(100.0);
    for i in 0u32..100_000 {
        td.add((i % 100) as f64); // uniform distribution [0, 100)
    }
    println!("Centroids: {}, total: {}", td.centroids.len(), td.total_count);
    println!("p50: {:?}", td.quantile(0.50));
    println!("p90: {:?}", td.quantile(0.90));
    println!("p99: {:?}", td.quantile(0.99));
}
```

### Rust-specific considerations

The `partition_point` method (stable since Rust 1.52) is idiomatic for binary search by predicate — equivalent to Go's `sort.Search`. It returns the first index where the predicate fails, which is exactly what T-Digest needs for insertion-point finding.

For production HyperLogLog in Rust, the `hyperloglog` crate provides a well-tested implementation. For Count-Min Sketch, the `sketches-ddsketch` crate includes DD-Sketch (a quantile estimator similar to T-Digest). For T-Digest specifically, `tdigest` crate is available.

The `DefaultHasher` in Rust is explicitly not stable across versions or platforms. For any probabilistic structure whose results are compared across processes (e.g., merging HyperLogLogs computed on different machines), pin the hash function to `fnv`, `ahash`, or `xxhash`.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| HyperLogLog merge | Simple register-wise max | Same |
| Count-Min insertion | Direct slice mutation | Same; iterator `.zip()` more idiomatic |
| T-Digest sort maintenance | `sort.Search` + manual insertion sort | `partition_point` + `Vec::insert` |
| Concurrency | `sync.RWMutex` for read-heavy structures | `Arc<RwLock<T>>` or sharded locks |
| Production libraries | No stdlib; Redis client uses FNV | `hyperloglog`, `sketches-ddsketch` crates |
| Hash stability | `hash/fnv` is stable | `DefaultHasher` is NOT stable; use `fnv` crate |

## Production War Stories

**Redis HyperLogLog** (Redis source: `hyperloglog.c`): Redis added `PFADD`/`PFCOUNT` commands (HyperLogLog) in Redis 2.8.9 (2014). The implementation uses m=2^14 = 16384 registers of 6 bits = 12 KB per key. The `PF` prefix comes from Philippe Flajolet, who invented HyperLogLog. Redis's implementation includes a dense/sparse encoding: HLLs with few distinct values use a sparse encoding (storing only non-zero registers as a sorted list), switching to dense (all 16384 registers) after ~6000 unique elements. This reduces memory for cold keys from 12 KB to ~1 KB.

**Apache DataSketches (formerly Yahoo Sketches)** (https://datasketches.apache.org): The DataSketches library provides production-hardened implementations of HLL, Count-Min Sketch, T-Digest, and Quantile Sketch (a different, more accurate alternative to T-Digest). Used by Apache Druid, Apache Spark, and Apache Pinot for approximate analytics at scale. Pinot's GROUP BY DISTINCT COUNT uses HLL; frequency histogram queries use Count-Min Sketch. The library is notable for providing formal accuracy guarantees (not just empirical benchmarks) and sketches that can be merged across distributed nodes.

**Prometheus native histograms** (Prometheus source: `model/histogram.go`): Prometheus 2.40 introduced "native histograms" using DDSketch (a relative-error quantile estimator similar to T-Digest but with stronger accuracy guarantees). The motivation: Prometheus's classic histograms require pre-specified buckets (e.g., [0.1ms, 1ms, 10ms, 100ms, 1000ms]). If latency distribution changes (a spike to 5000ms), the pre-specified buckets are useless. Native histograms adapt automatically: they store relative-error buckets so that any quantile can be estimated with bounded relative error, not absolute error.

## Complexity Analysis

| Structure | Space | Add | Query | Merge |
|-----------|-------|-----|-------|-------|
| HyperLogLog | O(m) = O(2^p) ≈ 1.5 × 2^p bytes | O(1) | O(m) for count | O(m) |
| Count-Min Sketch | O(d × w) = O(1/(εδ) × log(1/δ)) | O(d) | O(d) | O(d × w) |
| T-Digest | O(compression) centroids | O(log n) amortized | O(log n) | O((n1+n2) log(n1+n2)) |

HyperLogLog standard errors for different precisions:

| p | m = 2^p | Space | Std Error |
|---|---------|-------|-----------|
| 10 | 1024 | 768 B | 3.25% |
| 12 | 4096 | 3 KB | 1.625% |
| 14 | 16384 | 12 KB | 0.812% |
| 16 | 65536 | 48 KB | 0.406% |

Count-Min Sketch: for ε=0.001 (0.1% error), d=5 failure probability = e^(-5) ≈ 0.7%: w = 2718 counters per row, total = 5 × 2718 × 4 bytes = 54 KB. This handles a stream of 10 billion items with 0.1% error in 54 KB.

T-Digest: compression=100 gives approximately 100 centroids for typical distributions — 1.6 KB (100 × 16 bytes per centroid). Absolute error near p99 is typically < 0.1% of total count.

## Common Pitfalls

**Pitfall 1: Comparing HyperLogLog counts computed with different hash functions**

Two HLLs built with different hash functions cannot be merged and should not be compared directly. This comes up when one service uses FNV-64a and another uses MurmurHash3 — their `PFMERGE` result would be meaningless. Always standardize on one hash function for HLL across all components in a system.

**Pitfall 2: Querying items never added to Count-Min Sketch**

A Count-Min Sketch estimate for an item that was never added returns a value > 0 due to hash collisions. This is expected behavior — the structure estimates frequency "from above." The mistake is treating a non-zero estimate as confirmation that the item was seen. Use a Bloom filter as a pre-filter if you need to distinguish "never seen" from "seen with low frequency."

**Pitfall 3: T-Digest accuracy degrades when merging many sub-digests**

T-Digest supports merging: compute T-Digests on each shard, then merge centrally. However, multiple merge rounds (merge N digests into 1, then merge that result with M more digests) degrade accuracy because the centroid size invariant is checked per-merge, not globally. The merged digest may violate the quantile-weighted size constraint. Use a single merge from all shards rather than sequential merges.

**Pitfall 4: HyperLogLog undercounting for very small or very large cardinalities**

The raw harmonic mean estimator is inaccurate for n << m (few unique elements — linear counting correction needed) and for n >> 2^32 (wraparound correction needed). The Go implementation above includes both corrections. Many tutorial implementations omit them, giving 10-50% error near the extremes.

**Pitfall 5: Using T-Digest for absolute value estimation rather than quantile estimation**

T-Digest centroids store mean values with counts. The centroid's mean represents many actual values. Asking "what is the exact value at p99?" is meaningless — T-Digest estimates the quantile rank, not the exact value. For distributions with outliers (a single request taking 60 seconds while 99% are < 100ms), the p99.9 estimate can be significantly off if the outlier fell into a centroid far from its true rank.

## Exercises

**Exercise 1 — Verification** (30 min): Run the HyperLogLog implementation with p=10, 12, 14, 16. For each, add exactly 100,000 distinct elements and measure the relative error. Verify that the empirical standard deviation across 100 runs matches the theoretical value of 1.04/sqrt(m). Plot error vs precision.

**Exercise 2 — Extension** (2-4h): Implement distributed HyperLogLog counting: simulate 4 shards, each adding a disjoint subset of 250,000 unique users (total 1M unique). Merge the 4 HLLs and verify that the estimate is within 1% of 1M. Verify that the merge operation is order-independent (merging shards in any order produces the same result).

**Exercise 3 — From Scratch** (4-8h): Implement Count-Min Sketch with min-heap for heavy hitter detection. Maintain a heap of the top-k items by estimated frequency. After processing N items, the heap contains the estimated k most frequent items. Handle the case where a heavy hitter's estimated frequency decreases (it should never happen with conservative update, but verify). Compare against a sorted `map[string]int` frequency counter for correctness.

**Exercise 4 — Production Scenario** (8-15h): Build a latency monitoring system for a web server: generate synthetic request latencies drawn from a bimodal distribution (90% requests: 5-50ms; 10% requests: 100-2000ms). Use T-Digest to track p50, p95, p99, and p999 over a sliding window of the last 1 minute (reset the digest every minute). Compare the T-Digest estimates against exact quantiles computed from the raw latency array. Measure the memory savings: exact quantiles require storing all latencies (800 KB for 100K observations); T-Digest requires ~1.6 KB.

## Further Reading

### Foundational Papers
- Flajolet, P., Fusy, É., Gandouet, O., & Meunier, F. (2007). "HyperLogLog: The Analysis of a Near-Optimal Cardinality Estimation Algorithm." *AOFA 2007*. The defining paper; includes the bias correction terms and the dense/sparse encoding analysis.
- Cormode, G., & Muthukrishnan, S. (2005). "An Improved Data Stream Summary: The Count-Min Sketch and its Applications." *Journal of Algorithms*, 55(1), 58–75. Full complexity analysis with conservative update proof.
- Dunning, T., & Ertl, O. (2019). "Computing Extremely Accurate Quantiles Using t-Digests." arXiv:1902.04023. The canonical T-Digest paper with accuracy analysis and practical guidance.
- Ertl, O. (2017). "New cardinality estimation algorithms for HyperLogLog sketches." arXiv:1702.01284. Correction algorithms that improve HyperLogLog accuracy, especially near small counts.

### Books
- Muthukrishnan, S. (2005). *Data Streams: Algorithms and Applications*. Foundations and Trends in Theoretical Computer Science. The comprehensive survey of streaming algorithms; covers CMS, HLL precursors, and quantile estimation.
- Leskovec, J., Rajaraman, A., & Ullman, J. D. (2020). *Mining of Massive Datasets* (3rd ed.). Chapter 4 covers locality-sensitive hashing and probabilistic counting; freely available online.

### Production Code to Read
- `redis/src/hyperloglog.c` (https://github.com/antirez/redis) — Redis's HyperLogLog. Study the `hllSparseSet` / `hllDenseSet` functions for the sparse/dense representation switching.
- `apache/datasketches-java` (https://github.com/apache/datasketches-java) — The production-grade sketch library. `hll/HllArray.java` for HyperLogLog; `frequencies/ItemsSketch.java` for frequency estimation (a more accurate alternative to Count-Min Sketch for top-k queries).
- `tdunning/t-digest` (https://github.com/tdunning/t-digest) — The reference T-Digest implementation by the original author. Study `AVLTreeDigest.java` for the balanced BST centroid management.

### Conference Talks
- Dunning, T. (Spark Summit 2015): "Sketching Data: The Art of Approximate Computing" — practical guide to choosing between HLL, CMS, and T-Digest for different analytics problems.
- Cormode, G. (SIGMOD 2011): "Synopses for Massive Data" — tutorial on the theoretical foundations of data sketches including the Count-Min Sketch with lower bound proofs.
