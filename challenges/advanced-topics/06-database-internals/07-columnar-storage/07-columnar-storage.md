<!--
type: reference
difficulty: advanced
section: [06-database-internals]
concepts: [columnar-storage, rle, dictionary-encoding, delta-encoding, bitpacking, pax-layout, vectorized-execution, simd]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: analyze
prerequisites: [row-storage-basics, cache-architecture, simd-basics]
papers: [abadi-2008-column-stores, stonebraker-2005-c-store]
industry_use: [clickhouse, duckdb, parquet, arrow, vertica, redshift]
language_contrast: medium
-->

# Columnar Storage

> Columnar storage exists because aggregation queries only need 2 of a table's 50 columns, and reading 2 columns from disk is 25x cheaper than reading all 50 — but only if those 2 columns are stored contiguously rather than interleaved with the other 48.

## Mental Model

Row storage packs all columns of one row together on disk. Columnar storage packs all values of one column together. For OLTP queries that read a few complete rows (`SELECT * FROM orders WHERE id = 42`), row storage wins: one disk read fetches all 50 columns of row 42. For OLAP queries that aggregate across many rows but few columns (`SELECT SUM(revenue) FROM orders WHERE country = 'US'`), columnar storage wins: only the `revenue` and `country` columns need to be read, regardless of how many other columns exist.

The second advantage of columnar storage is compression. All values in one column have the same type and often similar magnitude. An integer column with values in [0, 100] wastes 3 bytes per value when stored as INT32 — bitpacking can store them in 7 bits each. A string column for `country` (250 possible values) can be dictionary-encoded: replace each string with a 1-byte integer code, then store the code column (1 byte per row instead of 2-10 bytes per string). A column with many repeated adjacent values can be run-length encoded: instead of storing 1,000,000 ones and then 500,000 zeros, store "1,000,000 × 1" and "500,000 × 0". These compression techniques are not just about space — they reduce the amount of data read from disk per query, which directly reduces query latency.

The third advantage is vectorized execution. A loop that processes 1,024 integers from a columnar array can be compiled to SIMD instructions that process 8 or 16 values per CPU clock cycle. Row-oriented processing has poor SIMD utilization because each row is a struct, and extracting one column's values from an array of structs requires strided memory access — the CPU's prefetcher cannot predict the stride pattern efficiently.

## Core Concepts

### Column Encodings

**Run-Length Encoding (RLE)**: Replaces sequences of identical values with (value, count) pairs.
```
Input:  [A, A, A, B, B, A, C, C, C, C]
RLE:    [(A,3), (B,2), (A,1), (C,4)]
```
RLE works best on sorted columns (or nearly-sorted). ClickHouse's `MergeTree` engine keeps data sorted by primary key, which means the primary key column (and correlated columns) have long runs — RLE achieves 100:1 or better compression ratios. For random-order columns, RLE increases size (each value needs a count field).

**Dictionary Encoding**: Replaces values with integer codes from a dictionary.
```
Input:  ["US", "UK", "US", "DE", "US", "UK"]
Dict:   {0: "US", 1: "UK", 2: "DE"}
Codes:  [0, 1, 0, 2, 0, 1]
```
A string column with 1,000 distinct countries and 100 million rows: storing strings (avg 8 bytes) = 800MB. Dictionary-coded (10 bits = 2 bytes per code + ~8KB dictionary) = 200MB + 8KB — 4x compression. The codes are also faster to filter and aggregate — integer comparison vs string comparison.

**Delta Encoding**: Stores differences between consecutive values instead of absolute values. For a sorted timestamp column with values in milliseconds:
```
Input:  [1000, 1050, 1120, 1200, 1350]
Delta:  [1000, 50, 70, 80, 150]      ← first value absolute, rest are differences
```
Delta encoding combined with bitpacking reduces the required bit width: if deltas are all < 256, 8-bit storage suffices even if the original values are 64-bit timestamps.

**Bitpacking**: Packs multiple small integers into a single machine word by allocating only the bits needed.
```
Values in [0, 100]: need ceil(log2(100)) = 7 bits each
Pack 8 values into 56 bits (vs 8 × 32 = 256 bits for INT32)
Compression ratio: 256/56 = 4.6x
```
SIMD bitpacking processes 128, 256, or 512 bits at once — packing/unpacking 16-64 values per instruction. This is the core of FastPFor, the compression algorithm used by Roaring Bitmaps and Apache Parquet's INT_PACKED encoding.

### PAX Layout: Hybrid Row-Column Storage

PAX (Partition Attributes Cross) is a hybrid layout that divides a table into fixed-size "mini-pages" (typically 64KB). Within each mini-page, data is stored columnar. Across mini-pages, data is stored as rows (each mini-page contains consecutive rows from the table).

```
PAX Layout (3 columns, 6 rows, 3 rows per mini-page):

Mini-page 0:
  col_a: [1, 2, 3]
  col_b: ["alice", "bob", "charlie"]
  col_c: [100, 200, 300]

Mini-page 1:
  col_a: [4, 5, 6]
  col_b: ["diana", "eve", "frank"]
  col_c: [400, 500, 600]
```

PAX benefits from columnar compression and SIMD within each mini-page while maintaining row-level locality — updating a row requires writing only one mini-page, not one page per column (which is the cost of a pure columnar layout).

### Vectorized Execution

Vectorized execution processes data in vectors of 1,024 values (typical SIMD width is 4-8 values per instruction × 256 values per batch × unrolling). A vectorized filter applies the predicate to a vector of column values, producing a selection vector (a list of row indices that pass the filter).

```
Column values (batch of 8): [10, 25, 5, 30, 15, 40, 20, 35]
Predicate: value > 20
Selection vector:           [1, 3, 5, 7]   (indices of values > 20)
```

The selection vector is passed to the next operator, which reads only the selected values from subsequent columns. This avoids materializing rows that will be filtered out.

SIMD instructions for columnar processing:
- AVX2 (256-bit): 8 × int32 operations per instruction
- AVX-512 (512-bit): 16 × int32 operations per instruction
- ARM NEON (128-bit): 4 × int32 operations per instruction

A well-written vectorized filter can process 4-8 billion integers per second on modern hardware — orders of magnitude faster than scalar row-by-row processing.

## Implementation: Go

```go
package main

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"os"
)

// ColumnType enumerates supported column types for serialization.
type ColumnType uint8

const (
	TypeInt32  ColumnType = 1
	TypeString ColumnType = 2
)

// RLEBlock represents a run-length encoded column segment.
// Stored on disk as: [num_runs(4)] [value(4) | count(4)] × num_runs
type RLEBlock struct {
	Runs []RLERun
}

type RLERun struct {
	Value int32
	Count uint32
}

// Encode serializes the RLE block to binary.
func (r *RLEBlock) Encode() []byte {
	buf := make([]byte, 4+len(r.Runs)*8)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(r.Runs)))
	for i, run := range r.Runs {
		offset := 4 + i*8
		binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(run.Value))
		binary.LittleEndian.PutUint32(buf[offset+4:offset+8], run.Count)
	}
	return buf
}

func DecodeRLEBlock(data []byte) *RLEBlock {
	numRuns := int(binary.LittleEndian.Uint32(data[0:4]))
	runs := make([]RLERun, numRuns)
	for i := 0; i < numRuns; i++ {
		offset := 4 + i*8
		runs[i] = RLERun{
			Value: int32(binary.LittleEndian.Uint32(data[offset : offset+4])),
			Count: binary.LittleEndian.Uint32(data[offset+4 : offset+8]),
		}
	}
	return &RLEBlock{Runs: runs}
}

// Decompress expands the RLE block to a flat slice of int32 values.
func (r *RLEBlock) Decompress() []int32 {
	total := uint32(0)
	for _, run := range r.Runs {
		total += run.Count
	}
	out := make([]int32, 0, total)
	for _, run := range r.Runs {
		for i := uint32(0); i < run.Count; i++ {
			out = append(out, run.Value)
		}
	}
	return out
}

// rleEncode encodes a sorted-or-near-sorted int32 slice.
func rleEncode(values []int32) *RLEBlock {
	if len(values) == 0 {
		return &RLEBlock{}
	}
	runs := []RLERun{{Value: values[0], Count: 1}}
	for i := 1; i < len(values); i++ {
		if values[i] == runs[len(runs)-1].Value {
			runs[len(runs)-1].Count++
		} else {
			runs = append(runs, RLERun{Value: values[i], Count: 1})
		}
	}
	return &RLEBlock{Runs: runs}
}

// DictionaryBlock stores a string column with dictionary encoding.
// On disk: [dict_size(4)] [entry_len(2) | entry_bytes] × dict_size
//          [num_codes(4)] [code(2)] × num_codes
type DictionaryBlock struct {
	Dict  []string
	Codes []uint16
}

func buildDictionary(values []string) *DictionaryBlock {
	// Build dictionary: assign codes in order of first occurrence
	codeMap := make(map[string]uint16)
	var dict []string
	codes := make([]uint16, len(values))
	for i, v := range values {
		code, ok := codeMap[v]
		if !ok {
			code = uint16(len(dict))
			codeMap[v] = code
			dict = append(dict, v)
		}
		codes[i] = code
	}
	return &DictionaryBlock{Dict: dict, Codes: codes}
}

func (d *DictionaryBlock) Encode() []byte {
	// Dict section
	var dictSection []byte
	for _, entry := range d.Dict {
		b := make([]byte, 2+len(entry))
		binary.LittleEndian.PutUint16(b[0:2], uint16(len(entry)))
		copy(b[2:], entry)
		dictSection = append(dictSection, b...)
	}

	// Code section
	codeSection := make([]byte, 4+len(d.Codes)*2)
	binary.LittleEndian.PutUint32(codeSection[0:4], uint32(len(d.Codes)))
	for i, code := range d.Codes {
		binary.LittleEndian.PutUint16(codeSection[4+i*2:4+i*2+2], code)
	}

	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(len(d.Dict)))

	return append(append(hdr, dictSection...), codeSection...)
}

// Lookup returns the string value for index i.
func (d *DictionaryBlock) Lookup(i int) string {
	return d.Dict[d.Codes[i]]
}

// VectorFilter applies a predicate to an int32 column and returns
// the indices of values satisfying the predicate.
// This is the core of vectorized execution: process values in batches.
// A production implementation would use SIMD instructions here.
func VectorFilter(values []int32, pred func(int32) bool) []int {
	// Process in batches of 8 to hint to the compiler for SIMD optimization.
	// In Rust, this becomes an explicit SIMD intrinsic.
	const batchSize = 8
	result := make([]int, 0, len(values)/10)

	i := 0
	for ; i+batchSize <= len(values); i += batchSize {
		// Unrolled loop — Go compiler may auto-vectorize this
		batch := values[i : i+batchSize]
		for j, v := range batch {
			if pred(v) {
				result = append(result, i+j)
			}
		}
	}
	// Process remainder
	for ; i < len(values); i++ {
		if pred(values[i]) {
			result = append(result, i)
		}
	}
	return result
}

// BitpackedBlock stores uint32 values using the minimum number of bits per value.
// On disk: [bit_width(1) | num_values(4) | packed_data...]
type BitpackedBlock struct {
	BitWidth  uint8
	NumValues uint32
	Data      []byte
}

func bitpackEncode(values []uint32) *BitpackedBlock {
	if len(values) == 0 {
		return &BitpackedBlock{}
	}
	// Find max value to determine bit width
	maxVal := uint32(0)
	for _, v := range values {
		if v > maxVal {
			maxVal = v
		}
	}
	bitWidth := uint8(bits.Len32(maxVal))
	if bitWidth == 0 {
		bitWidth = 1
	}

	totalBits := int(bitWidth) * len(values)
	dataBytes := (totalBits + 7) / 8
	data := make([]byte, dataBytes)

	// Pack values: write bitWidth bits per value, MSB to LSB
	bitPos := 0
	for _, v := range values {
		for b := int(bitWidth) - 1; b >= 0; b-- {
			bit := (v >> b) & 1
			byteIdx := bitPos / 8
			bitOffset := 7 - (bitPos % 8)
			data[byteIdx] |= byte(bit) << bitOffset
			bitPos++
		}
	}

	return &BitpackedBlock{
		BitWidth:  bitWidth,
		NumValues: uint32(len(values)),
		Data:      data,
	}
}

func (b *BitpackedBlock) Decode() []uint32 {
	result := make([]uint32, b.NumValues)
	bitPos := 0
	for i := range result {
		var val uint32
		for bit := int(b.BitWidth) - 1; bit >= 0; bit-- {
			byteIdx := bitPos / 8
			bitOffset := 7 - (bitPos % 8)
			v := (b.Data[byteIdx] >> bitOffset) & 1
			val |= uint32(v) << bit
			bitPos++
		}
		result[i] = val
	}
	return result
}

func (b *BitpackedBlock) Encode() []byte {
	buf := make([]byte, 1+4+len(b.Data))
	buf[0] = b.BitWidth
	binary.LittleEndian.PutUint32(buf[1:5], b.NumValues)
	copy(buf[5:], b.Data)
	return buf
}

// ColumnarSegment is one segment of a table stored in columnar format.
// Matches the PAX "mini-page" concept: a fixed number of rows stored as separate column arrays.
type ColumnarSegment struct {
	NumRows int
	Columns map[string]interface{} // column name → encoded block
}

// ColumnarTable manages a columnar table split into segments.
type ColumnarTable struct {
	segments []*ColumnarSegment
	colTypes map[string]ColumnType
}

func NewColumnarTable() *ColumnarTable {
	return &ColumnarTable{
		colTypes: make(map[string]ColumnType),
	}
}

// AppendSegment adds a segment from map[colName][]interface{} values.
func (t *ColumnarTable) AppendSegment(rows map[string][]interface{}) {
	numRows := 0
	for _, col := range rows {
		numRows = len(col)
		break
	}

	seg := &ColumnarSegment{
		NumRows: numRows,
		Columns: make(map[string]interface{}),
	}

	for name, values := range rows {
		switch vs := interface{}(values).(type) {
		case []interface{}:
			// Detect type from first non-nil value
			for _, v := range vs {
				switch v.(type) {
				case int32:
					ints := make([]int32, len(vs))
					for i, val := range vs {
						ints[i] = val.(int32)
					}
					seg.Columns[name] = rleEncode(ints)
					t.colTypes[name] = TypeInt32
				case string:
					strs := make([]string, len(vs))
					for i, val := range vs {
						strs[i] = val.(string)
					}
					seg.Columns[name] = buildDictionary(strs)
					t.colTypes[name] = TypeString
				}
				break
			}
		}
	}
	t.segments = append(t.segments, seg)
}

// QuerySum computes SUM(numCol) WHERE filterCol = filterVal using vectorized execution.
// This is the OLAP query pattern that columnar storage optimizes.
func (t *ColumnarTable) QuerySum(filterCol, numCol string, filterVal string) int64 {
	sum := int64(0)
	for _, seg := range t.segments {
		filterBlk, ok := seg.Columns[filterCol].(*DictionaryBlock)
		if !ok {
			continue
		}
		numBlk, ok := seg.Columns[numCol].(*RLEBlock)
		if !ok {
			continue
		}

		// Find the dictionary code for filterVal — O(1) lookup after building reverse map
		filterCode := uint16(0xFFFF) // sentinel: not found
		for code, val := range filterBlk.Dict {
			if val == filterVal {
				filterCode = uint16(code)
				break
			}
		}
		if filterCode == 0xFFFF {
			continue // value not in this segment's dictionary
		}

		// Decompress numeric column for this segment
		nums := numBlk.Decompress()

		// Vectorized filter + aggregate: process codes and accumulate sum
		// In production this would be a SIMD loop
		for i, code := range filterBlk.Codes {
			if code == filterCode {
				sum += int64(nums[i])
			}
		}
	}
	return sum
}

func main() {
	// Demonstrate each encoding
	fmt.Println("=== RLE Encoding ===")
	values := []int32{1, 1, 1, 2, 2, 3, 3, 3, 3, 3, 4}
	rle := rleEncode(values)
	encoded := rle.Encode()
	decoded := rle.Decompress()
	fmt.Printf("Original: %v (%d bytes)\n", values, len(values)*4)
	fmt.Printf("RLE runs: %d runs (%d bytes)\n", len(rle.Runs), len(encoded))
	fmt.Printf("Decoded:  %v\n", decoded)

	fmt.Println("\n=== Dictionary Encoding ===")
	countries := []string{"US", "US", "UK", "DE", "US", "FR", "US", "UK"}
	dict := buildDictionary(countries)
	fmt.Printf("Original strings: %v\n", countries)
	fmt.Printf("Dictionary: %v\n", dict.Dict)
	fmt.Printf("Codes: %v\n", dict.Codes)
	fmt.Printf("Storage: %d bytes (vs %d bytes original)\n",
		len(dict.Encode()), totalStringBytes(countries))

	fmt.Println("\n=== Bitpacked Encoding ===")
	ages := []uint32{25, 30, 22, 45, 18, 67, 33, 28}
	bp := bitpackEncode(ages)
	bpEncoded := bp.Encode()
	bpDecoded := bp.Decode()
	fmt.Printf("Original ages: %v (%d bytes as uint32)\n", ages, len(ages)*4)
	fmt.Printf("Bit width: %d bits, packed: %d bytes\n", bp.BitWidth, len(bpEncoded))
	fmt.Printf("Decoded: %v\n", bpDecoded)

	fmt.Println("\n=== Columnar OLAP Query ===")
	table := NewColumnarTable()

	// Simulate a 1000-row sales table with country and revenue columns
	countryValues := make([]interface{}, 1000)
	revenueValues := make([]interface{}, 1000)
	countriesList := []string{"US", "UK", "DE", "FR", "JP"}
	for i := 0; i < 1000; i++ {
		countryValues[i] = countriesList[i%5]
		revenueValues[i] = int32((i % 100) + 10)
	}
	table.AppendSegment(map[string][]interface{}{
		"country": countryValues,
		"revenue": revenueValues,
	})

	totalRevenue := table.QuerySum("country", "revenue", "US")
	fmt.Printf("SUM(revenue) WHERE country='US': %d\n", totalRevenue)

	fmt.Println("\n=== Vectorized Filter ===")
	testVals := []int32{5, 25, 10, 35, 15, 45, 20, 55}
	indices := VectorFilter(testVals, func(v int32) bool { return v > 20 })
	fmt.Printf("Values > 20 at indices: %v\n", indices)
	selectedVals := make([]int32, len(indices))
	for i, idx := range indices {
		selectedVals[i] = testVals[idx]
	}
	fmt.Printf("Selected values: %v\n", selectedVals)

	// Write encoded data to show on-disk format
	f, _ := os.Create("/tmp/columnar_demo.bin")
	defer f.Close()
	rleBytes := rle.Encode()
	f.Write(rleBytes)
	dictBytes := dict.Encode()
	f.Write(dictBytes)
	fmt.Printf("\nWrote %d bytes of encoded column data to /tmp/columnar_demo.bin\n",
		len(rleBytes)+len(dictBytes))
}

func totalStringBytes(strs []string) int {
	total := 0
	for _, s := range strs {
		total += len(s)
	}
	return total
}
```

### Go-specific considerations

Go does not provide direct access to SIMD intrinsics from idiomatic Go code. The `VectorFilter` function processes values in batches of 8, which hints to the Go compiler's auto-vectorizer. For guaranteed SIMD, the options are: (1) use `cgo` to call C code with SSE/AVX intrinsics, (2) write assembly using Go's `//go:noescape` and plan9 assembly dialect, or (3) use a library like `gonum` that provides SIMD-optimized numerical operations.

For production columnar analytics in Go, the Apache Arrow Go library (`github.com/apache/arrow/go/arrow`) provides SIMD-optimized columnar operations. It implements dictionary encoding, RLE, and vectorized filters using either assembly or cgo on supported platforms.

The `interface{}` column values introduce boxing overhead that would be unacceptable in a production columnar store. In production, use type-specific slices (`[]int32`, `[]string`) and avoid interface{} anywhere in the hot query path. Go generics can help here but require careful design to avoid heap-allocating values.

## Implementation: Rust

```rust
use std::collections::HashMap;

// ---- RLE Encoding ----

#[derive(Debug, Clone)]
struct RLEBlock {
    runs: Vec<(i32, u32)>, // (value, count)
}

impl RLEBlock {
    fn encode(values: &[i32]) -> Self {
        if values.is_empty() { return RLEBlock { runs: vec![] }; }
        let mut runs = vec![(values[0], 1u32)];
        for &v in &values[1..] {
            if v == runs.last().unwrap().0 {
                runs.last_mut().unwrap().1 += 1;
            } else {
                runs.push((v, 1));
            }
        }
        RLEBlock { runs }
    }

    fn decompress(&self) -> Vec<i32> {
        let total: u32 = self.runs.iter().map(|(_, c)| c).sum();
        let mut out = Vec::with_capacity(total as usize);
        for &(val, count) in &self.runs {
            for _ in 0..count { out.push(val); }
        }
        out
    }

    fn to_bytes(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(4 + self.runs.len() * 8);
        out.extend_from_slice(&(self.runs.len() as u32).to_le_bytes());
        for &(val, count) in &self.runs {
            out.extend_from_slice(&(val as u32).to_le_bytes());
            out.extend_from_slice(&count.to_le_bytes());
        }
        out
    }

    fn compression_ratio(&self, original_len: usize) -> f64 {
        let encoded_bytes = 4 + self.runs.len() * 8;
        (original_len * 4) as f64 / encoded_bytes as f64
    }
}

// ---- Dictionary Encoding ----

#[derive(Debug, Clone)]
struct DictionaryBlock {
    dict:  Vec<String>,
    codes: Vec<u16>,
}

impl DictionaryBlock {
    fn encode(values: &[&str]) -> Self {
        let mut dict_map: HashMap<&str, u16> = HashMap::new();
        let mut dict: Vec<String> = Vec::new();
        let codes: Vec<u16> = values.iter().map(|&v| {
            *dict_map.entry(v).or_insert_with(|| {
                let code = dict.len() as u16;
                dict.push(v.to_string());
                code
            })
        }).collect();
        DictionaryBlock { dict, codes }
    }

    fn get(&self, i: usize) -> &str {
        &self.dict[self.codes[i] as usize]
    }

    fn find_code(&self, value: &str) -> Option<u16> {
        self.dict.iter().position(|v| v == value).map(|i| i as u16)
    }

    fn storage_bytes(&self) -> usize {
        4 + self.dict.iter().map(|s| 2 + s.len()).sum::<usize>() + self.codes.len() * 2
    }
}

// ---- Bitpacked Encoding ----

struct BitpackedBlock {
    bit_width:  u8,
    num_values: u32,
    data:       Vec<u8>,
}

impl BitpackedBlock {
    fn encode(values: &[u32]) -> Self {
        if values.is_empty() {
            return BitpackedBlock { bit_width: 0, num_values: 0, data: vec![] };
        }
        let max_val = *values.iter().max().unwrap();
        let bit_width = (u32::BITS - max_val.leading_zeros()).max(1) as u8;
        let total_bits = bit_width as usize * values.len();
        let data_bytes = (total_bits + 7) / 8;
        let mut data = vec![0u8; data_bytes];

        let mut bit_pos = 0usize;
        for &v in values {
            for b in (0..bit_width as usize).rev() {
                let bit = ((v >> b) & 1) as u8;
                let byte_idx = bit_pos / 8;
                let bit_offset = 7 - (bit_pos % 8);
                data[byte_idx] |= bit << bit_offset;
                bit_pos += 1;
            }
        }
        BitpackedBlock { bit_width, num_values: values.len() as u32, data }
    }

    fn decode(&self) -> Vec<u32> {
        let mut result = vec![0u32; self.num_values as usize];
        let mut bit_pos = 0usize;
        for v in result.iter_mut() {
            for b in (0..self.bit_width as usize).rev() {
                let byte_idx = bit_pos / 8;
                let bit_offset = 7 - (bit_pos % 8);
                let bit = ((self.data[byte_idx] >> bit_offset) & 1) as u32;
                *v |= bit << b;
                bit_pos += 1;
            }
        }
        result
    }

    fn bytes_used(&self) -> usize {
        1 + 4 + self.data.len()
    }
}

// ---- Vectorized Filter with SIMD hint ----
// The #[target_feature(enable = "avx2")] attribute enables AVX2 intrinsics.
// For safe cross-platform code, use portable SIMD via std::simd (nightly)
// or the `wide` crate. Here we show the scalar version with a SIMD note.

fn vector_filter_gt(values: &[i32], threshold: i32) -> Vec<usize> {
    // Process in chunks of 8 to hint at auto-vectorization.
    // With `-C target-cpu=native`, rustc may emit AVX2 vpcmpgtd for this loop.
    let mut result = Vec::with_capacity(values.len() / 8);
    for (i, &v) in values.iter().enumerate() {
        if v > threshold {
            result.push(i);
        }
    }
    result
}

// Vectorized SUM with selection vector
fn vector_sum_selected(values: &[i32], indices: &[usize]) -> i64 {
    // Process 4 at a time for auto-vectorization
    let mut sum = 0i64;
    for &i in indices {
        sum += values[i] as i64;
    }
    sum
}

// ---- Columnar Segment (PAX mini-page) ----

struct ColumnarSegment {
    num_rows: usize,
    int_cols:  HashMap<String, RLEBlock>,
    str_cols:  HashMap<String, DictionaryBlock>,
}

impl ColumnarSegment {
    fn new(
        int_data: HashMap<&str, Vec<i32>>,
        str_data: HashMap<&str, Vec<&str>>,
    ) -> Self {
        let num_rows = int_data.values().next()
            .map(|v| v.len())
            .or_else(|| str_data.values().next().map(|v| v.len()))
            .unwrap_or(0);

        let int_cols = int_data.iter()
            .map(|(&name, values)| (name.to_string(), RLEBlock::encode(values)))
            .collect();

        let str_cols = str_data.iter()
            .map(|(&name, values)| (name.to_string(), DictionaryBlock::encode(values)))
            .collect();

        ColumnarSegment { num_rows, int_cols, str_cols }
    }

    // query_sum computes SUM(num_col) WHERE str_col = value — the canonical OLAP pattern
    fn query_sum(&self, filter_col: &str, filter_val: &str, num_col: &str) -> i64 {
        let dict_blk = match self.str_cols.get(filter_col) {
            Some(b) => b,
            None => return 0,
        };
        let code = match dict_blk.find_code(filter_val) {
            Some(c) => c,
            None => return 0, // value not in this segment
        };
        let nums = self.int_cols.get(num_col)
            .map(|b| b.decompress())
            .unwrap_or_default();

        // Vectorized: collect matching indices, then sum their values
        let matching: Vec<usize> = dict_blk.codes.iter().enumerate()
            .filter(|(_, &c)| c == code)
            .map(|(i, _)| i)
            .collect();

        vector_sum_selected(&nums, &matching)
    }
}

fn main() {
    println!("=== RLE Encoding ===");
    let values = vec![1i32, 1, 1, 2, 2, 3, 3, 3, 3, 3, 4];
    let rle = RLEBlock::encode(&values);
    println!("Original: {:?} ({} bytes)", values, values.len() * 4);
    println!("Runs: {} runs ({} bytes)", rle.runs.len(), rle.to_bytes().len());
    println!("Compression ratio: {:.2}x", rle.compression_ratio(values.len()));
    println!("Decoded: {:?}", rle.decompress());

    println!("\n=== Dictionary Encoding ===");
    let countries = vec!["US", "US", "UK", "DE", "US", "FR", "US", "UK"];
    let dict = DictionaryBlock::encode(&countries);
    let orig_bytes: usize = countries.iter().map(|s| s.len()).sum();
    println!("Dictionary: {:?}", dict.dict);
    println!("Codes: {:?}", dict.codes);
    println!("Storage: {} bytes (vs {} bytes original)", dict.storage_bytes(), orig_bytes);

    println!("\n=== Bitpacked Encoding ===");
    let ages: Vec<u32> = vec![25, 30, 22, 45, 18, 67, 33, 28];
    let bp = BitpackedBlock::encode(&ages);
    println!("Original: {:?} ({} bytes as u32)", ages, ages.len() * 4);
    println!("Bit width: {}, packed: {} bytes", bp.bit_width, bp.bytes_used());
    println!("Decoded: {:?}", bp.decode());

    println!("\n=== Vectorized Filter ===");
    let test_vals: Vec<i32> = vec![5, 25, 10, 35, 15, 45, 20, 55];
    let indices = vector_filter_gt(&test_vals, 20);
    let selected: Vec<i32> = indices.iter().map(|&i| test_vals[i]).collect();
    println!("Values > 20: {:?}", selected);

    println!("\n=== Columnar OLAP Query ===");
    let n = 1000usize;
    let country_list = ["US", "UK", "DE", "FR", "JP"];
    let countries_col: Vec<&str> = (0..n).map(|i| country_list[i % 5]).collect();
    let revenue_col: Vec<i32> = (0..n).map(|i| ((i % 100) + 10) as i32).collect();

    let mut int_data = HashMap::new();
    int_data.insert("revenue", revenue_col);
    let mut str_data = HashMap::new();
    str_data.insert("country", countries_col);

    let seg = ColumnarSegment::new(int_data, str_data);
    let total = seg.query_sum("country", "US", "revenue");
    println!("SUM(revenue) WHERE country='US': {}", total);

    // Storage breakdown
    let rle_bytes = seg.int_cols.get("revenue").map(|b| b.to_bytes().len()).unwrap_or(0);
    let dict_bytes = seg.str_cols.get("country").map(|b| b.storage_bytes()).unwrap_or(0);
    println!("Revenue column (RLE): {} bytes", rle_bytes);
    println!("Country column (dict): {} bytes", dict_bytes);
    println!("Total columnar: {} bytes (vs {} bytes row-store)", rle_bytes + dict_bytes, n * 8);
}
```

### Rust-specific considerations

The `#[target_feature(enable = "avx2")]` attribute gates a function to only run when AVX2 is available at runtime. Combined with `is_x86_feature_detected!("avx2")` for a runtime check, this allows writing both a SIMD-accelerated and a fallback path. The `wide` crate (version 0.7+) provides a safe, portable SIMD API that compiles to the best available instruction set without unsafe code.

For production columnar processing in Rust, the `arrow2` crate (or the official `arrow` crate from Apache Arrow) provides dictionary-encoded arrays, RLE arrays, and vectorized filters with SIMD acceleration. The `DataFusion` query engine (also from Apache Arrow project) demonstrates how these primitives compose into a full vectorized execution engine.

Rust's `Vec<i32>` decompressed column is a contiguous heap allocation — ideal for SIMD processing, since the values are packed without any padding. In contrast, a row-oriented approach would require a stride of (row_size / column_width) for every column access.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| SIMD access | No direct intrinsics; auto-vectorization hints | `#[target_feature]` + unsafe intrinsics or `wide`/`std::simd` crates |
| Column type safety | `interface{}` requires runtime type assertions | Typed structs per encoding; zero overhead |
| Bitpacking | Pure Go; portable but no SIMD | Can use SIMD; `crate::wide::i32x8` for vectorized ops |
| Column compression ratio | Same algorithms, same ratios | Same — compression is algorithm-dependent not language-dependent |
| Apache Arrow integration | `github.com/apache/arrow/go` | `crate::arrow2` or `crate::arrow` (Apache official) |
| Allocation overhead | GC manages decompressed slices | Explicit lifetime; decompressed `Vec<i32>` freed when out of scope |

## Production War Stories

**ClickHouse's MergeTree and compression cascading**: ClickHouse's MergeTree engine applies a compression pipeline per column: first encode with a codec (RLE, delta, delta-of-delta for time series), then compress the encoded bytes with LZ4 or ZSTD. A time-series metrics column with values like [1000, 1001, 1003, 1002, ...] is delta-encoded to [1000, 1, 2, -1, ...], and the delta values (small integers near zero) compress dramatically with LZ4. A production ClickHouse cluster commonly achieves 10-20x compression on time-series data. The storage savings are not just about cost — they mean more data fits in the OS page cache, which means more queries are served from memory rather than disk.

**DuckDB's vectorized execution and columnar in-memory processing**: DuckDB processes data in "morsel-driven" vectorized batches of 1024 tuples. Its filter kernel uses SIMD to compare 8 or 16 values per instruction. A query `SELECT SUM(revenue) FROM sales WHERE country = 'US'` on a 1-billion-row table can process 10 billion values per second on a single modern CPU (with AVX-512). The key insight in DuckDB's architecture: never materialize intermediate row batches — keep data in columnar format throughout the entire query pipeline, converting to rows only for the final output if needed. This avoids "materialization tax" — the cost of packing/unpacking row-structured intermediate results.

**Apache Parquet's dictionary encoding and predicate pushdown**: Parquet files store row groups (typically 128MB each), and within each row group, each column has a dictionary page (if the column has few distinct values) and data pages. A query with `WHERE country = 'US'` can be answered for each Parquet row group by: (1) reading only the country column's dictionary page (a few KB), (2) checking if 'US' appears in the dictionary, (3) if not, skipping the entire row group (no data pages read). This is "predicate pushdown to storage" — the filtering happens at the dictionary level, not after reading all values. For a 10TB dataset where 80% of row groups are for non-US countries, this avoids reading 8TB of data.

## Complexity Analysis

| Operation | Row Store | Columnar |
|-----------|-----------|----------|
| Point read (all columns) | O(1) — one page read | O(columns) — one page read per column |
| Column scan (1 column) | O(n × row_size / page_size) | O(n × col_size / page_size) |
| Filtered aggregate (2/50 cols) | O(n × 50/page) I/O | O(n × 2/page) I/O — 25x less I/O |
| Dictionary filter (O(1) per row) | O(n) string comparisons | O(n) integer comparisons + O(1) dict lookup |
| RLE aggregate (k runs) | O(n) | O(k) — process runs, not individual values |
| Write (INSERT) | O(1) — append to row | O(columns) — update each column file |

The 25x I/O reduction for a 2-column query over a 50-column table is the headline number for columnar storage. In practice, the reduction is 5-15x for typical OLAP workloads with queries touching 5-10 columns of 50-100 column tables.

RLE's O(k) aggregate is the hidden gem: if a column has `k` distinct runs and you want the SUM over all rows, you process `k` (value, count) pairs rather than `n` individual values. For a timestamp column sorted by day with 365 days of data and 1 million rows per day, `k = 365` and `n = 365,000,000` — three orders of magnitude difference.

## Common Pitfalls

**Pitfall 1: Using columnar storage for OLTP write-heavy workloads**

Columnar storage makes writes expensive: a single row insert must append to each column's storage file separately (one write per column). For 50 columns, that is 50 writes instead of 1. Some hybrid systems (ClickHouse, SingleStore) maintain an in-memory row buffer that is periodically merged into columnar storage — similar to LSM-tree's MemTable pattern. If you are running an OLTP workload with thousands of single-row inserts per second, columnar storage will bottleneck on writes. The correct choice for mixed OLTP+OLAP is an HTAP system (TiDB, Yellowbrick) or a separate OLAP replica.

**Pitfall 2: Dictionary encoding degrading on high-cardinality columns**

Dictionary encoding is effective only when the number of distinct values is much smaller than the total row count. A column like `user_id` with 100 million distinct values in a 100 million-row table cannot be dictionary-encoded (the dictionary is as large as the data). Parquet's adaptive encoding uses dictionary encoding only when the dictionary fits in a configurable limit (default 1MB). Columns with cardinality > 10% of row count should use delta encoding, bitpacking, or no encoding.

**Pitfall 3: Decompressing entire columns for filtered queries**

A naive columnar implementation decompresses the entire column before filtering. The correct approach is late materialization: apply filters on compressed representations where possible (check if a value is in the RLE range or the dictionary code matches before decompressing), and decompress only the values that pass the filter. ClickHouse's SIMD-accelerated filter applies predicates directly to the compressed data when the encoding allows it (dictionary codes are integers and can be compared with SIMD integer instructions without decompression).

**Pitfall 4: Not aligning column data to SIMD register boundaries**

SIMD loads require data to be aligned to the register size boundary (16 bytes for SSE, 32 bytes for AVX2, 64 bytes for AVX-512). Unaligned loads are slower (on older Intel CPUs, 50% slower; on recent CPUs, typically no penalty). In Rust, allocating column buffers with `std::alloc::alloc` using a layout aligned to 64 bytes ensures AVX-512 compatibility. `Vec<i32>` in Rust is aligned to `align_of::<i32>()` = 4 bytes, which is insufficient for AVX2.

**Pitfall 5: Ignoring zone maps (min/max statistics) for pruning**

Each column chunk in a columnar file can store min/max statistics. A query `WHERE revenue > 10000` can skip any column chunk whose `max(revenue) < 10000` entirely. This is "zone map pruning" or "data skipping." Columnar formats that do not implement zone maps (or implementations that forget to consult them at query time) miss a major performance opportunity. In production columnar stores, 70-90% of data skipping comes from zone maps, not Bloom filters.

## Exercises

**Exercise 1** (30 min): Benchmark the columnar `QuerySum` in Go against an equivalent row-oriented implementation (`[]struct{country string; revenue int32}` scanned linearly). Use 10 million rows. Measure time, memory allocations, and compute the effective scan rate (bytes/second). Observe the columnar advantage for the 2-column query.

**Exercise 2** (2-4h): Implement delta encoding in Rust for a sorted timestamp column. The format: `[first_value(8) | delta_bit_width(1) | num_values(4) | bitpacked_deltas...]`. Verify that a sorted timestamp column with 1 million values at 1-second intervals (timestamps in nanoseconds) compresses from 8MB to under 1MB.

**Exercise 3** (4-8h): Implement a PAX layout in Go: fixed mini-pages of 64KB, each mini-page stores columns separately. Write a benchmark comparing PAX vs pure row store for a mixed workload: 80% point-read queries (benefit row store), 20% aggregate queries (benefit columnar). Verify that PAX outperforms both extremes on the mixed workload.

**Exercise 4** (8-15h): Implement vectorized execution with SIMD in Rust using the `wide` crate. Implement a vectorized filter (`i32x8 > threshold`) and a vectorized sum (`i32x8` horizontal add). Benchmark the SIMD version against the scalar version on 100 million integers at different threshold selectivities (1%, 10%, 50%, 90%). Plot speedup vs selectivity.

## Further Reading

### Foundational Papers
- Abadi, D. et al. (2008). "Column-Stores vs. Row-Stores: How Different Are They Really?" *SIGMOD*, 967–980. The definitive comparison with benchmarks.
- Stonebraker, M. et al. (2005). "C-Store: A Column-oriented DBMS." *VLDB*, 553–564. The academic precursor to Vertica.
- Willhalm, T. et al. (2009). "SIMD-Scan: Ultra Fast in-Memory Table Scan using On-Chip Vector Processing Units." *PVLDB*, 2(1), 385–394. SIMD for vectorized filters.

### Books
- Petrov, A. (2019). *Database Internals*. O'Reilly. Chapter 14 covers column stores and compression.
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. Chapter 3 covers columnar storage in the context of OLAP systems.

### Production Code to Read
- `ClickHouse/src/Compression/` — compression pipeline with codecs
- `duckdb/src/execution/operator/aggregate/` — vectorized aggregate operators
- `apache/parquet-format/` — Parquet file format specification with encoding details
- `apache/arrow/cpp/src/arrow/compute/kernels/` — Arrow compute kernels with SIMD

### Talks
- Neumann, T. (VLDB 2011): "Efficiently Compiling Efficient Query Plans for Modern Hardware" — vectorized vs compiled execution
- Mühlbauer, T. et al. (VLDB 2013): "Instant Loading for Main Memory Databases" — how vectorized loading eliminates parsing overhead
