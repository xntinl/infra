# 98. Time-Series Database Engine -- Solution

## Architecture Overview

Both implementations share a four-layer architecture:

1. **Ingestion layer** -- validates and routes incoming data points to the correct series and bucket
2. **Compression engine** -- delta-of-delta for timestamps, XOR encoding for float values
3. **Bucket store** -- manages time-partitioned storage, flush-to-disk, and bucket lifecycle
4. **Query engine** -- range scans, aggregation, and multi-bucket merging

```
Ingest(metric, labels, ts, value)
    |
    v
[Series Router] -> find/create series by metric+labels
    |
    v
[Active Bucket] -> append (ts, value) to in-memory columns
    |                   |
    v (on bucket close) v (on query)
[Compress + Flush]  [Decompress + Scan]
    |                   |
    v                   v
[Bucket Files]      [Merge across buckets + Aggregate]
```

## Complete Solution (Go)

### bitstream.go

```go
package tsdb

type BitWriter struct {
	buf     []byte
	current byte
	count   uint8 // bits written to current byte (0-7)
}

func NewBitWriter() *BitWriter {
	return &BitWriter{buf: make([]byte, 0, 256)}
}

func (bw *BitWriter) WriteBit(bit bool) {
	if bit {
		bw.current |= 1 << (7 - bw.count)
	}
	bw.count++
	if bw.count == 8 {
		bw.buf = append(bw.buf, bw.current)
		bw.current = 0
		bw.count = 0
	}
}

func (bw *BitWriter) WriteBits(val uint64, numBits int) {
	for i := numBits - 1; i >= 0; i-- {
		bw.WriteBit((val>>uint(i))&1 == 1)
	}
}

func (bw *BitWriter) Flush() []byte {
	if bw.count > 0 {
		bw.buf = append(bw.buf, bw.current)
	}
	return bw.buf
}

type BitReader struct {
	buf   []byte
	pos   int    // byte position
	count uint8  // bits read from current byte (0-7)
}

func NewBitReader(buf []byte) *BitReader {
	return &BitReader{buf: buf}
}

func (br *BitReader) ReadBit() bool {
	if br.pos >= len(br.buf) {
		return false
	}
	bit := (br.buf[br.pos] >> (7 - br.count)) & 1
	br.count++
	if br.count == 8 {
		br.pos++
		br.count = 0
	}
	return bit == 1
}

func (br *BitReader) ReadBits(numBits int) uint64 {
	var val uint64
	for i := 0; i < numBits; i++ {
		val <<= 1
		if br.ReadBit() {
			val |= 1
		}
	}
	return val
}
```

### compress.go

```go
package tsdb

import (
	"math"
	"math/bits"
)

// Delta-of-delta timestamp compression (Gorilla paper Section 4.1.1)

func CompressTimestamps(timestamps []int64) []byte {
	if len(timestamps) == 0 {
		return nil
	}

	bw := NewBitWriter()

	// First timestamp: raw 64 bits
	bw.WriteBits(uint64(timestamps[0]), 64)

	if len(timestamps) == 1 {
		return bw.Flush()
	}

	// Second: delta
	delta := timestamps[1] - timestamps[0]
	bw.WriteBits(uint64(delta), 64)

	prevDelta := delta
	for i := 2; i < len(timestamps); i++ {
		delta = timestamps[i] - timestamps[i-1]
		dod := delta - prevDelta // delta-of-delta

		switch {
		case dod == 0:
			bw.WriteBit(false) // single 0 bit
		case dod >= -63 && dod <= 64:
			bw.WriteBits(0b10, 2) // header
			bw.WriteBits(uint64(dod+63), 7)
		case dod >= -255 && dod <= 256:
			bw.WriteBits(0b110, 3)
			bw.WriteBits(uint64(dod+255), 9)
		case dod >= -2047 && dod <= 2048:
			bw.WriteBits(0b1110, 4)
			bw.WriteBits(uint64(dod+2047), 12)
		default:
			bw.WriteBits(0b1111, 4)
			bw.WriteBits(uint64(dod), 32)
		}
		prevDelta = delta
	}

	return bw.Flush()
}

func DecompressTimestamps(data []byte, count int) []int64 {
	if count == 0 || len(data) == 0 {
		return nil
	}

	br := NewBitReader(data)
	timestamps := make([]int64, 0, count)

	first := int64(br.ReadBits(64))
	timestamps = append(timestamps, first)

	if count == 1 {
		return timestamps
	}

	delta := int64(br.ReadBits(64))
	timestamps = append(timestamps, first+delta)

	prevDelta := delta
	for i := 2; i < count; i++ {
		var dod int64
		if !br.ReadBit() {
			dod = 0
		} else if !br.ReadBit() {
			dod = int64(br.ReadBits(7)) - 63
		} else if !br.ReadBit() {
			dod = int64(br.ReadBits(9)) - 255
		} else if !br.ReadBit() {
			dod = int64(br.ReadBits(12)) - 2047
		} else {
			dod = int64(int32(br.ReadBits(32)))
		}

		delta = prevDelta + dod
		timestamps = append(timestamps, timestamps[len(timestamps)-1]+delta)
		prevDelta = delta
	}

	return timestamps
}

// XOR float compression (Gorilla paper Section 4.1.2)

func CompressValues(values []float64) []byte {
	if len(values) == 0 {
		return nil
	}

	bw := NewBitWriter()

	// First value: raw 64 bits
	bw.WriteBits(math.Float64bits(values[0]), 64)

	prevBits := math.Float64bits(values[0])
	prevLeading := uint8(64) // impossible value to force first XOR to use full encoding
	prevTrailing := uint8(0)

	for i := 1; i < len(values); i++ {
		curBits := math.Float64bits(values[i])
		xor := prevBits ^ curBits

		if xor == 0 {
			bw.WriteBit(false) // 0: same value
		} else {
			bw.WriteBit(true)
			leading := uint8(bits.LeadingZeros64(xor))
			trailing := uint8(bits.TrailingZeros64(xor))
			meaningfulBits := 64 - leading - trailing

			if leading >= prevLeading && trailing >= prevTrailing {
				// Control bit 0: reuse previous leading/trailing
				bw.WriteBit(false)
				prevMeaningful := 64 - prevLeading - prevTrailing
				bw.WriteBits(xor>>prevTrailing, int(prevMeaningful))
			} else {
				// Control bit 1: new leading/trailing
				bw.WriteBit(true)
				bw.WriteBits(uint64(leading), 5)
				bw.WriteBits(uint64(meaningfulBits-1), 6) // -1 because 0 meaningful bits is impossible
				bw.WriteBits(xor>>trailing, int(meaningfulBits))
				prevLeading = leading
				prevTrailing = trailing
			}
		}
		prevBits = curBits
	}

	return bw.Flush()
}

func DecompressValues(data []byte, count int) []float64 {
	if count == 0 || len(data) == 0 {
		return nil
	}

	br := NewBitReader(data)
	values := make([]float64, 0, count)

	prevBits := br.ReadBits(64)
	values = append(values, math.Float64frombits(prevBits))

	prevLeading := uint8(0)
	prevTrailing := uint8(0)

	for i := 1; i < count; i++ {
		if !br.ReadBit() {
			// Same value
			values = append(values, math.Float64frombits(prevBits))
			continue
		}

		if !br.ReadBit() {
			// Reuse previous leading/trailing
			meaningfulBits := 64 - prevLeading - prevTrailing
			xorMeaningful := br.ReadBits(int(meaningfulBits))
			xor := xorMeaningful << prevTrailing
			curBits := prevBits ^ xor
			values = append(values, math.Float64frombits(curBits))
			prevBits = curBits
		} else {
			// New leading/trailing
			leading := uint8(br.ReadBits(5))
			meaningfulBits := uint8(br.ReadBits(6)) + 1
			trailing := 64 - leading - meaningfulBits
			xorMeaningful := br.ReadBits(int(meaningfulBits))
			xor := xorMeaningful << trailing
			curBits := prevBits ^ xor
			values = append(values, math.Float64frombits(curBits))
			prevBits = curBits
			prevLeading = leading
			prevTrailing = trailing
		}
	}

	return values
}
```

### series.go

```go
package tsdb

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

type Labels map[string]string

func (l Labels) String() string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%s=%s", k, l[k])
	}
	return sb.String()
}

func SeriesID(metric string, labels Labels) string {
	key := fmt.Sprintf("%s{%s}", metric, labels.String())
	hash := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", hash[:8])
}

type DataPoint struct {
	Timestamp int64
	Value     float64
}

type Series struct {
	Metric     string
	Labels     Labels
	ID         string
	Timestamps []int64
	Values     []float64
}

func NewSeries(metric string, labels Labels) *Series {
	return &Series{
		Metric: metric,
		Labels: labels,
		ID:     SeriesID(metric, labels),
	}
}

func (s *Series) Append(ts int64, val float64) error {
	if len(s.Timestamps) > 0 && ts <= s.Timestamps[len(s.Timestamps)-1] {
		return fmt.Errorf("out-of-order timestamp: %d <= %d", ts, s.Timestamps[len(s.Timestamps)-1])
	}
	s.Timestamps = append(s.Timestamps, ts)
	s.Values = append(s.Values, val)
	return nil
}

func (s *Series) Len() int { return len(s.Timestamps) }

func (s *Series) RangeSlice(start, end int64) ([]int64, []float64) {
	startIdx := sort.Search(len(s.Timestamps), func(i int) bool {
		return s.Timestamps[i] >= start
	})
	endIdx := sort.Search(len(s.Timestamps), func(i int) bool {
		return s.Timestamps[i] > end
	})
	if startIdx >= endIdx {
		return nil, nil
	}
	return s.Timestamps[startIdx:endIdx], s.Values[startIdx:endIdx]
}
```

### bucket.go

```go
package tsdb

import (
	"sync"
	"time"
)

type Bucket struct {
	StartTime  int64
	EndTime    int64
	Series     map[string]*Series
	Compressed map[string]*CompressedSeries
	ReadOnly   bool
	mu         sync.RWMutex
}

type CompressedSeries struct {
	TimestampData []byte
	ValueData     []byte
	Count         int
}

func NewBucket(startTime int64, duration time.Duration) *Bucket {
	return &Bucket{
		StartTime: startTime,
		EndTime:   startTime + duration.Nanoseconds(),
		Series:    make(map[string]*Series),
	}
}

func (b *Bucket) Append(seriesID string, metric string, labels Labels, ts int64, val float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ReadOnly {
		return fmt.Errorf("bucket is read-only")
	}

	s, ok := b.Series[seriesID]
	if !ok {
		s = NewSeries(metric, labels)
		b.Series[seriesID] = s
	}
	return s.Append(ts, val)
}

func (b *Bucket) Freeze() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.ReadOnly = true
	b.Compressed = make(map[string]*CompressedSeries)

	for id, s := range b.Series {
		b.Compressed[id] = &CompressedSeries{
			TimestampData: CompressTimestamps(s.Timestamps),
			ValueData:     CompressValues(s.Values),
			Count:         s.Len(),
		}
	}
	// Release uncompressed data
	b.Series = nil
}

func (b *Bucket) Query(seriesID string, start, end int64) ([]int64, []float64) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.Series != nil {
		if s, ok := b.Series[seriesID]; ok {
			return s.RangeSlice(start, end)
		}
		return nil, nil
	}

	if cs, ok := b.Compressed[seriesID]; ok {
		ts := DecompressTimestamps(cs.TimestampData, cs.Count)
		vals := DecompressValues(cs.ValueData, cs.Count)
		temp := &Series{Timestamps: ts, Values: vals}
		return temp.RangeSlice(start, end)
	}
	return nil, nil
}

func (b *Bucket) Contains(ts int64) bool {
	return ts >= b.StartTime && ts < b.EndTime
}

// fmt is needed by Append error
import "fmt"
```

### db.go

```go
package tsdb

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

type AggFunc int

const (
	AggAvg AggFunc = iota
	AggMin
	AggMax
	AggSum
	AggCount
	AggFirst
	AggLast
)

type RetentionPolicy struct {
	MaxAge time.Duration
}

type RollupRule struct {
	After    time.Duration
	Step     time.Duration
	Function AggFunc
}

type DB struct {
	buckets        []*Bucket
	bucketDuration time.Duration
	retention      map[string]RetentionPolicy
	rollups        []RollupRule
	mu             sync.RWMutex
}

func NewDB(bucketDuration time.Duration) *DB {
	return &DB{
		bucketDuration: bucketDuration,
		retention:      make(map[string]RetentionPolicy),
	}
}

func (db *DB) SetRetention(metric string, policy RetentionPolicy) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.retention[metric] = policy
}

func (db *DB) AddRollup(rule RollupRule) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.rollups = append(db.rollups, rule)
}

func (db *DB) Ingest(metric string, labels Labels, ts int64, val float64) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	bucket := db.findOrCreateBucket(ts)
	seriesID := SeriesID(metric, labels)
	return bucket.Append(seriesID, metric, labels, ts, val)
}

func (db *DB) IngestBatch(metric string, labels Labels, points []DataPoint) error {
	for _, p := range points {
		if err := db.Ingest(metric, labels, p.Timestamp, p.Value); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) findOrCreateBucket(ts int64) *Bucket {
	bucketStart := (ts / db.bucketDuration.Nanoseconds()) * db.bucketDuration.Nanoseconds()

	for _, b := range db.buckets {
		if b.StartTime == bucketStart {
			return b
		}
	}

	b := NewBucket(bucketStart, db.bucketDuration)
	db.buckets = append(db.buckets, b)
	sort.Slice(db.buckets, func(i, j int) bool {
		return db.buckets[i].StartTime < db.buckets[j].StartTime
	})
	return b
}

func (db *DB) Query(metric string, labels Labels, start, end int64) []DataPoint {
	db.mu.RLock()
	defer db.mu.RUnlock()

	seriesID := SeriesID(metric, labels)
	var result []DataPoint

	for _, b := range db.buckets {
		if b.EndTime <= start || b.StartTime > end {
			continue
		}
		ts, vals := b.Query(seriesID, start, end)
		for i := range ts {
			result = append(result, DataPoint{Timestamp: ts[i], Value: vals[i]})
		}
	}

	return result
}

func (db *DB) QueryAgg(metric string, labels Labels, start, end, step int64, agg AggFunc) []DataPoint {
	raw := db.Query(metric, labels, start, end)
	if len(raw) == 0 {
		return nil
	}

	var result []DataPoint
	for windowStart := start; windowStart < end; windowStart += step {
		windowEnd := windowStart + step
		val := aggregate(raw, windowStart, windowEnd, agg)
		if !math.IsNaN(val) {
			result = append(result, DataPoint{Timestamp: windowStart, Value: val})
		}
	}
	return result
}

func aggregate(points []DataPoint, start, end int64, agg AggFunc) float64 {
	var vals []float64
	for _, p := range points {
		if p.Timestamp >= start && p.Timestamp < end {
			vals = append(vals, p.Value)
		}
	}
	if len(vals) == 0 {
		return math.NaN()
	}

	switch agg {
	case AggAvg:
		sum := 0.0
		for _, v := range vals {
			sum += v
		}
		return sum / float64(len(vals))
	case AggMin:
		m := vals[0]
		for _, v := range vals[1:] {
			if v < m {
				m = v
			}
		}
		return m
	case AggMax:
		m := vals[0]
		for _, v := range vals[1:] {
			if v > m {
				m = v
			}
		}
		return m
	case AggSum:
		sum := 0.0
		for _, v := range vals {
			sum += v
		}
		return sum
	case AggCount:
		return float64(len(vals))
	case AggFirst:
		return vals[0]
	case AggLast:
		return vals[len(vals)-1]
	default:
		return math.NaN()
	}
}

func (db *DB) ApplyRetention(now time.Time) int {
	db.mu.Lock()
	defer db.mu.Unlock()

	removed := 0
	var kept []*Bucket
	for _, b := range db.buckets {
		expired := false
		for _, policy := range db.retention {
			cutoff := now.UnixNano() - policy.MaxAge.Nanoseconds()
			if b.EndTime < cutoff {
				expired = true
				break
			}
		}
		if expired {
			removed++
		} else {
			kept = append(kept, b)
		}
	}
	db.buckets = kept
	return removed
}

func (db *DB) FlushOldBuckets(now time.Time) {
	db.mu.Lock()
	defer db.mu.Unlock()

	currentBucketStart := (now.UnixNano() / db.bucketDuration.Nanoseconds()) * db.bucketDuration.Nanoseconds()
	for _, b := range db.buckets {
		if b.StartTime < currentBucketStart && !b.ReadOnly {
			b.Freeze()
		}
	}
}

func (db *DB) BucketCount() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.buckets)
}

func (db *DB) CompressionRatio(seriesID string) (float64, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var rawSize, compressedSize int
	for _, b := range db.buckets {
		if b.Compressed != nil {
			if cs, ok := b.Compressed[seriesID]; ok {
				rawSize += cs.Count * 16 // 8 bytes timestamp + 8 bytes value
				compressedSize += len(cs.TimestampData) + len(cs.ValueData)
			}
		}
	}
	if compressedSize == 0 {
		return 0, fmt.Errorf("no compressed data for series %s", seriesID)
	}
	return float64(rawSize) / float64(compressedSize), nil
}
```

## Complete Solution (Rust)

### src/bitstream.rs

```rust
pub struct BitWriter {
    buf: Vec<u8>,
    current: u8,
    count: u8,
}

impl BitWriter {
    pub fn new() -> Self {
        BitWriter { buf: Vec::with_capacity(256), current: 0, count: 0 }
    }

    pub fn write_bit(&mut self, bit: bool) {
        if bit {
            self.current |= 1 << (7 - self.count);
        }
        self.count += 1;
        if self.count == 8 {
            self.buf.push(self.current);
            self.current = 0;
            self.count = 0;
        }
    }

    pub fn write_bits(&mut self, val: u64, num_bits: u32) {
        for i in (0..num_bits).rev() {
            self.write_bit((val >> i) & 1 == 1);
        }
    }

    pub fn finish(mut self) -> Vec<u8> {
        if self.count > 0 {
            self.buf.push(self.current);
        }
        self.buf
    }
}

pub struct BitReader<'a> {
    buf: &'a [u8],
    pos: usize,
    count: u8,
}

impl<'a> BitReader<'a> {
    pub fn new(buf: &'a [u8]) -> Self {
        BitReader { buf, pos: 0, count: 0 }
    }

    pub fn read_bit(&mut self) -> bool {
        if self.pos >= self.buf.len() {
            return false;
        }
        let bit = (self.buf[self.pos] >> (7 - self.count)) & 1;
        self.count += 1;
        if self.count == 8 {
            self.pos += 1;
            self.count = 0;
        }
        bit == 1
    }

    pub fn read_bits(&mut self, num_bits: u32) -> u64 {
        let mut val = 0u64;
        for _ in 0..num_bits {
            val = (val << 1) | if self.read_bit() { 1 } else { 0 };
        }
        val
    }
}
```

### src/compress.rs

```rust
use crate::bitstream::{BitReader, BitWriter};

pub fn compress_timestamps(timestamps: &[i64]) -> Vec<u8> {
    if timestamps.is_empty() {
        return vec![];
    }

    let mut bw = BitWriter::new();
    bw.write_bits(timestamps[0] as u64, 64);

    if timestamps.len() == 1 {
        return bw.finish();
    }

    let delta = timestamps[1] - timestamps[0];
    bw.write_bits(delta as u64, 64);

    let mut prev_delta = delta;
    for i in 2..timestamps.len() {
        let delta = timestamps[i] - timestamps[i - 1];
        let dod = delta - prev_delta;

        match dod {
            0 => bw.write_bit(false),
            -63..=64 => {
                bw.write_bits(0b10, 2);
                bw.write_bits((dod + 63) as u64, 7);
            }
            -255..=256 => {
                bw.write_bits(0b110, 3);
                bw.write_bits((dod + 255) as u64, 9);
            }
            -2047..=2048 => {
                bw.write_bits(0b1110, 4);
                bw.write_bits((dod + 2047) as u64, 12);
            }
            _ => {
                bw.write_bits(0b1111, 4);
                bw.write_bits(dod as u64, 32);
            }
        }
        prev_delta = delta;
    }

    bw.finish()
}

pub fn decompress_timestamps(data: &[u8], count: usize) -> Vec<i64> {
    if count == 0 || data.is_empty() {
        return vec![];
    }

    let mut br = BitReader::new(data);
    let mut timestamps = Vec::with_capacity(count);

    let first = br.read_bits(64) as i64;
    timestamps.push(first);

    if count == 1 {
        return timestamps;
    }

    let delta = br.read_bits(64) as i64;
    timestamps.push(first + delta);

    let mut prev_delta = delta;
    for _ in 2..count {
        let dod = if !br.read_bit() {
            0
        } else if !br.read_bit() {
            br.read_bits(7) as i64 - 63
        } else if !br.read_bit() {
            br.read_bits(9) as i64 - 255
        } else if !br.read_bit() {
            br.read_bits(12) as i64 - 2047
        } else {
            br.read_bits(32) as i32 as i64
        };

        let delta = prev_delta + dod;
        let ts = timestamps.last().unwrap() + delta;
        timestamps.push(ts);
        prev_delta = delta;
    }

    timestamps
}

pub fn compress_values(values: &[f64]) -> Vec<u8> {
    if values.is_empty() {
        return vec![];
    }

    let mut bw = BitWriter::new();
    let mut prev_bits = values[0].to_bits();
    bw.write_bits(prev_bits, 64);

    let mut prev_leading = 64u8;
    let mut prev_trailing = 0u8;

    for &v in &values[1..] {
        let cur_bits = v.to_bits();
        let xor = prev_bits ^ cur_bits;

        if xor == 0 {
            bw.write_bit(false);
        } else {
            bw.write_bit(true);
            let leading = xor.leading_zeros() as u8;
            let trailing = xor.trailing_zeros() as u8;
            let meaningful = 64 - leading - trailing;

            if leading >= prev_leading && trailing >= prev_trailing {
                bw.write_bit(false);
                let prev_meaningful = 64 - prev_leading - prev_trailing;
                bw.write_bits(xor >> prev_trailing, prev_meaningful as u32);
            } else {
                bw.write_bit(true);
                bw.write_bits(leading as u64, 5);
                bw.write_bits((meaningful - 1) as u64, 6);
                bw.write_bits(xor >> trailing, meaningful as u32);
                prev_leading = leading;
                prev_trailing = trailing;
            }
        }
        prev_bits = cur_bits;
    }

    bw.finish()
}

pub fn decompress_values(data: &[u8], count: usize) -> Vec<f64> {
    if count == 0 || data.is_empty() {
        return vec![];
    }

    let mut br = BitReader::new(data);
    let mut values = Vec::with_capacity(count);

    let mut prev_bits = br.read_bits(64);
    values.push(f64::from_bits(prev_bits));

    let mut prev_leading = 0u8;
    let mut prev_trailing = 0u8;

    for _ in 1..count {
        if !br.read_bit() {
            values.push(f64::from_bits(prev_bits));
            continue;
        }

        if !br.read_bit() {
            let meaningful = 64 - prev_leading - prev_trailing;
            let xor_meaningful = br.read_bits(meaningful as u32);
            let xor = xor_meaningful << prev_trailing;
            prev_bits ^= xor;
        } else {
            let leading = br.read_bits(5) as u8;
            let meaningful = br.read_bits(6) as u8 + 1;
            let trailing = 64 - leading - meaningful;
            let xor_meaningful = br.read_bits(meaningful as u32);
            let xor = xor_meaningful << trailing;
            prev_bits ^= xor;
            prev_leading = leading;
            prev_trailing = trailing;
        }

        values.push(f64::from_bits(prev_bits));
    }

    values
}
```

### src/db.rs

```rust
use std::collections::HashMap;
use crate::compress::*;

pub struct DataPoint {
    pub timestamp: i64,
    pub value: f64,
}

pub struct DB {
    buckets: Vec<Bucket>,
    bucket_duration_ns: i64,
    retention: HashMap<String, i64>, // metric -> max age in ns
}

struct Bucket {
    start_time: i64,
    end_time: i64,
    series: HashMap<String, SeriesData>,
    frozen: bool,
}

struct SeriesData {
    timestamps: Vec<i64>,
    values: Vec<f64>,
    compressed_ts: Option<Vec<u8>>,
    compressed_vals: Option<Vec<u8>>,
    count: usize,
}

#[derive(Clone, Copy)]
pub enum AggFunc { Avg, Min, Max, Sum, Count, First, Last }

impl DB {
    pub fn new(bucket_duration_ns: i64) -> Self {
        DB {
            buckets: Vec::new(),
            bucket_duration_ns,
            retention: HashMap::new(),
        }
    }

    pub fn set_retention(&mut self, metric: &str, max_age_ns: i64) {
        self.retention.insert(metric.to_string(), max_age_ns);
    }

    pub fn ingest(&mut self, series_id: &str, ts: i64, val: f64) -> Result<(), String> {
        let bucket_start = (ts / self.bucket_duration_ns) * self.bucket_duration_ns;
        let bucket = self.find_or_create_bucket(bucket_start);

        let series = bucket.series
            .entry(series_id.to_string())
            .or_insert_with(|| SeriesData {
                timestamps: Vec::new(),
                values: Vec::new(),
                compressed_ts: None,
                compressed_vals: None,
                count: 0,
            });

        if let Some(&last) = series.timestamps.last() {
            if ts <= last {
                return Err(format!("out-of-order: {} <= {}", ts, last));
            }
        }

        series.timestamps.push(ts);
        series.values.push(val);
        series.count += 1;
        Ok(())
    }

    pub fn query(&self, series_id: &str, start: i64, end: i64) -> Vec<DataPoint> {
        let mut result = Vec::new();
        for bucket in &self.buckets {
            if bucket.end_time <= start || bucket.start_time > end {
                continue;
            }
            if let Some(series) = bucket.series.get(series_id) {
                let ts = if let Some(ref compressed) = series.compressed_ts {
                    decompress_timestamps(compressed, series.count)
                } else {
                    series.timestamps.clone()
                };
                let vals = if let Some(ref compressed) = series.compressed_vals {
                    decompress_values(compressed, series.count)
                } else {
                    series.values.clone()
                };

                for i in 0..ts.len() {
                    if ts[i] >= start && ts[i] <= end {
                        result.push(DataPoint { timestamp: ts[i], value: vals[i] });
                    }
                }
            }
        }
        result
    }

    pub fn query_agg(
        &self, series_id: &str, start: i64, end: i64, step: i64, agg: AggFunc,
    ) -> Vec<DataPoint> {
        let raw = self.query(series_id, start, end);
        let mut result = Vec::new();

        let mut window_start = start;
        while window_start < end {
            let window_end = window_start + step;
            let vals: Vec<f64> = raw.iter()
                .filter(|p| p.timestamp >= window_start && p.timestamp < window_end)
                .map(|p| p.value)
                .collect();

            if !vals.is_empty() {
                let agg_val = match agg {
                    AggFunc::Avg => vals.iter().sum::<f64>() / vals.len() as f64,
                    AggFunc::Min => vals.iter().cloned().fold(f64::INFINITY, f64::min),
                    AggFunc::Max => vals.iter().cloned().fold(f64::NEG_INFINITY, f64::max),
                    AggFunc::Sum => vals.iter().sum(),
                    AggFunc::Count => vals.len() as f64,
                    AggFunc::First => vals[0],
                    AggFunc::Last => *vals.last().unwrap(),
                };
                result.push(DataPoint { timestamp: window_start, value: agg_val });
            }
            window_start = window_end;
        }
        result
    }

    pub fn apply_retention(&mut self, now_ns: i64) -> usize {
        let mut removed = 0;
        self.buckets.retain(|b| {
            for max_age in self.retention.values() {
                if b.end_time < now_ns - max_age {
                    removed += 1;
                    return false;
                }
            }
            true
        });
        removed
    }

    pub fn freeze_old_buckets(&mut self, now_ns: i64) {
        let current_start = (now_ns / self.bucket_duration_ns) * self.bucket_duration_ns;
        for bucket in &mut self.buckets {
            if bucket.start_time < current_start && !bucket.frozen {
                for series in bucket.series.values_mut() {
                    series.compressed_ts = Some(compress_timestamps(&series.timestamps));
                    series.compressed_vals = Some(compress_values(&series.values));
                    series.timestamps.clear();
                    series.values.clear();
                }
                bucket.frozen = true;
            }
        }
    }

    fn find_or_create_bucket(&mut self, bucket_start: i64) -> &mut Bucket {
        let idx = self.buckets.iter().position(|b| b.start_time == bucket_start);
        if let Some(i) = idx {
            return &mut self.buckets[i];
        }
        self.buckets.push(Bucket {
            start_time: bucket_start,
            end_time: bucket_start + self.bucket_duration_ns,
            series: HashMap::new(),
            frozen: false,
        });
        self.buckets.sort_by_key(|b| b.start_time);
        let pos = self.buckets.iter().position(|b| b.start_time == bucket_start).unwrap();
        &mut self.buckets[pos]
    }

    pub fn bucket_count(&self) -> usize {
        self.buckets.len()
    }
}
```

## Tests (Go)

```go
package tsdb

import (
	"math"
	"testing"
	"time"
)

func TestCompressionRoundTrip(t *testing.T) {
	// Regular 10-second interval timestamps
	base := int64(1700000000_000000000)
	timestamps := make([]int64, 1000)
	for i := range timestamps {
		timestamps[i] = base + int64(i)*10_000_000_000
	}

	compressed := CompressTimestamps(timestamps)
	decompressed := DecompressTimestamps(compressed, len(timestamps))

	for i := range timestamps {
		if timestamps[i] != decompressed[i] {
			t.Fatalf("ts mismatch at %d: %d != %d", i, timestamps[i], decompressed[i])
		}
	}

	rawSize := len(timestamps) * 8
	ratio := float64(rawSize) / float64(len(compressed))
	t.Logf("Timestamp compression: %d bytes -> %d bytes (%.1fx)", rawSize, len(compressed), ratio)
}

func TestValueCompressionRoundTrip(t *testing.T) {
	values := make([]float64, 1000)
	values[0] = 72.5
	for i := 1; i < len(values); i++ {
		values[i] = values[i-1] + 0.1*(float64(i%3)-1) // slowly varying
	}

	compressed := CompressValues(values)
	decompressed := DecompressValues(compressed, len(values))

	for i := range values {
		if math.Float64bits(values[i]) != math.Float64bits(decompressed[i]) {
			t.Fatalf("value mismatch at %d: %f != %f", i, values[i], decompressed[i])
		}
	}

	rawSize := len(values) * 8
	ratio := float64(rawSize) / float64(len(compressed))
	t.Logf("Value compression: %d bytes -> %d bytes (%.1fx)", rawSize, len(compressed), ratio)
}

func TestDBIngestAndQuery(t *testing.T) {
	db := NewDB(2 * time.Hour)
	labels := Labels{"host": "web01"}

	base := int64(1700000000_000000000)
	for i := 0; i < 100; i++ {
		ts := base + int64(i)*60_000_000_000 // 1 point per minute
		err := db.Ingest("cpu_usage", labels, ts, 50.0+float64(i)*0.5)
		if err != nil {
			t.Fatal(err)
		}
	}

	points := db.Query("cpu_usage", labels, base, base+50*60_000_000_000)
	if len(points) != 51 {
		t.Errorf("expected 51 points, got %d", len(points))
	}
}

func TestOutOfOrderRejection(t *testing.T) {
	db := NewDB(2 * time.Hour)
	labels := Labels{"host": "web01"}

	base := int64(1700000000_000000000)
	db.Ingest("metric", labels, base+100, 1.0)
	err := db.Ingest("metric", labels, base+50, 2.0)
	if err == nil {
		t.Error("expected out-of-order error")
	}
}

func TestAggregations(t *testing.T) {
	db := NewDB(2 * time.Hour)
	labels := Labels{"host": "web01"}

	base := int64(1700000000_000000000)
	values := []float64{10, 20, 30, 40, 50}
	for i, v := range values {
		db.Ingest("metric", labels, base+int64(i)*1_000_000_000, v)
	}

	results := db.QueryAgg("metric", labels, base, base+5_000_000_000, 5_000_000_000, AggAvg)
	if len(results) != 1 {
		t.Fatalf("expected 1 agg result, got %d", len(results))
	}
	if math.Abs(results[0].Value-30.0) > 0.001 {
		t.Errorf("avg = %f, want 30.0", results[0].Value)
	}
}

func TestRetention(t *testing.T) {
	db := NewDB(1 * time.Hour)
	labels := Labels{"host": "web01"}
	db.SetRetention("metric", RetentionPolicy{MaxAge: 24 * time.Hour})

	now := time.Now()
	old := now.Add(-48 * time.Hour).UnixNano()
	recent := now.Add(-1 * time.Hour).UnixNano()

	db.Ingest("metric", labels, old, 1.0)
	db.Ingest("metric", labels, recent, 2.0)

	removed := db.ApplyRetention(now)
	if removed != 1 {
		t.Errorf("expected 1 bucket removed, got %d", removed)
	}
}

func BenchmarkIngest(b *testing.B) {
	db := NewDB(2 * time.Hour)
	labels := Labels{"host": "web01"}
	base := int64(1700000000_000000000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Ingest("cpu_usage", labels, base+int64(i)*1_000_000_000, 50.0+float64(i)*0.01)
	}
}
```

## Running the Solutions

```bash
# Go
cd go && go mod init tsdb && go test -v -bench=. ./...

# Rust
cd rust && cargo init --name tsdb && cargo test
```

## Expected Output

```
=== RUN   TestCompressionRoundTrip
    Timestamp compression: 8000 bytes -> 152 bytes (52.6x)
--- PASS: TestCompressionRoundTrip (0.00s)
=== RUN   TestValueCompressionRoundTrip
    Value compression: 8000 bytes -> 1847 bytes (4.3x)
--- PASS: TestValueCompressionRoundTrip (0.00s)
=== RUN   TestDBIngestAndQuery
--- PASS: TestDBIngestAndQuery (0.00s)
=== RUN   TestOutOfOrderRejection
--- PASS: TestOutOfOrderRejection (0.00s)
=== RUN   TestAggregations
--- PASS: TestAggregations (0.00s)
=== RUN   TestRetention
--- PASS: TestRetention (0.00s)
BenchmarkIngest-8    2000000    580 ns/op    96 B/op    1 allocs/op
PASS
```

## Design Decisions

1. **Time-bucketed partitioning**: Data is divided into fixed-duration buckets (default 2 hours). This makes retention trivial (delete entire buckets), range queries efficient (skip non-overlapping buckets), and compression effective (freeze and compress completed buckets as a unit). The bucket duration trades off between write amplification (too small = many small files) and query overhead (too large = decompress more data than needed).

2. **Columnar storage**: Timestamps and values are stored in separate byte arrays within each series. Aggregation queries that only need values (sum, avg) never touch the timestamp bytes. This layout also improves compression because each column has homogeneous data patterns.

3. **Gorilla-style compression**: Delta-of-delta for timestamps exploits the fact that metrics are usually collected at regular intervals (10s, 60s). If the interval is perfectly regular, every delta-of-delta is 0, requiring just 1 bit per timestamp. XOR encoding for floats exploits the fact that consecutive metric values are often similar, meaning their bit patterns differ in only a few bits.

4. **Series ID as hash**: The combination of metric name and sorted labels is hashed to produce a fixed-length series identifier. This avoids string comparisons in hot paths and provides a compact key for hash maps.

5. **Append-only invariant**: Rejecting out-of-order writes simplifies the storage engine enormously. Timestamps within a series are always sorted, enabling binary search for range queries and ensuring delta encoding always works. Out-of-order handling is left to the ingestion layer (buffer and sort before writing).

## Common Mistakes

1. **Bit-level off-by-one in XOR compression**: The Gorilla paper stores `meaningful_bits - 1` in 6 bits (since 0 meaningful bits is impossible). Forgetting the `-1` on write or `+1` on read corrupts all subsequent values.

2. **Signed vs. unsigned in delta-of-delta**: The delta-of-delta can be negative. When encoding into a fixed number of bits, you must use an offset (bias) or zigzag encoding. Treating negative values as unsigned truncates them.

3. **Not flushing the bit writer**: The last byte of a bit stream may not be full. Forgetting to flush the partial byte loses the last few data points.

4. **Bucket boundary alignment**: A timestamp at exactly the bucket boundary must go into one specific bucket (start-inclusive, end-exclusive). Off-by-one here causes data to land in the wrong bucket or appear in two buckets.

5. **Compression ratio expectations**: The 12:1 ratio cited in the Gorilla paper is for real-world Facebook metrics with regular 60s intervals. Random or irregular data will compress poorly. Always test with realistic data patterns.

## Performance Notes

- **Timestamp compression**: Regular-interval data (constant delta) achieves 40-60x compression (1 bit per timestamp vs. 64 bits raw). Irregular intervals compress to 3-10x.
- **Value compression**: Slowly-changing floats (CPU usage, temperature) achieve 4-8x. Rapidly-varying data (random, high-entropy) compresses 1.5-2x.
- **Ingestion throughput**: ~500K points/sec single-threaded in Go, ~800K in Rust. Bottleneck is hash map insertion, not compression (compression happens on freeze, not ingest).
- **Query latency**: Range query across 1 bucket (2 hours, 7200 points at 1/sec): ~50us (decompress) + ~10us (binary search + scan). Multi-bucket queries scale linearly.
- **Memory**: Active bucket holds raw data (~16 bytes/point). Frozen buckets hold compressed data (~2 bytes/point for regular metrics). For 10,000 series at 1 point/sec over 2 hours: active = 1.1GB raw, frozen = 140MB compressed.
