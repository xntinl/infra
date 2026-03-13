# Exercise 12: Performance -- JSON vs Protobuf vs MessagePack

**Difficulty:** Insane | **Estimated Time:** 60 minutes | **Section:** 18 - Encoding

## Challenge

Build a comprehensive benchmarking suite that compares serialization formats across multiple dimensions: speed, size, allocation pressure, and schema evolution. You will implement the same data model in four formats and measure everything.

## Requirements

### Formats to Compare

1. **encoding/json** -- standard library JSON
2. **protobuf** -- `google.golang.org/protobuf` with a `.proto` definition
3. **MessagePack** -- `github.com/vmihailenco/msgpack/v5`
4. **easyjson** -- `github.com/mailru/easyjson` (code-generated JSON, optional stretch goal)

### Data Model

Use a realistic API payload -- an e-commerce order:

```go
type Order struct {
    ID         string
    CustomerID string
    Items      []OrderItem
    Shipping   Address
    Billing    Address
    Total      float64
    Currency   string
    Status     string
    CreatedAt  time.Time
    Metadata   map[string]string
}

type OrderItem struct {
    ProductID   string
    Name        string
    Quantity    int32
    PricePerUnit float64
    Discount     float64
}

type Address struct {
    Line1   string
    Line2   string
    City    string
    State   string
    Zip     string
    Country string
}
```

Create an equivalent `.proto` file for the protobuf version.

### Benchmark Requirements

Write Go benchmarks (`testing.B`) that measure:

1. **Marshal speed** -- `BenchmarkMarshalJSON`, `BenchmarkMarshalProto`, `BenchmarkMarshalMsgpack`
2. **Unmarshal speed** -- same pattern
3. **Round-trip** -- marshal then unmarshal
4. **Allocation count** -- use `b.ReportAllocs()`
5. **Output size** -- print encoded sizes outside the benchmark loop

Use a consistent test fixture: generate one order with 10 items and realistic data. Use `b.ResetTimer()` after setup.

### Analysis Program

Write a `main.go` (separate from benchmarks) that:

1. Creates the test order
2. Encodes in each format
3. Prints a comparison table:

```
Format      Size (bytes)  Marshal (ns)  Unmarshal (ns)  Allocs/op
JSON        1247          3420          5180            42
Protobuf    389           890           1120            12
MessagePack 612           1560          2340            23
```

Use `testing.Benchmark` or manual timing with `time.Now()` and averaging over 10000 iterations.

4. Validates round-trip correctness for each format

### Schema Evolution Test

Demonstrate backward/forward compatibility:

1. Define a "v2" of the order that adds a `Priority` field and removes `Metadata`
2. Encode a v1 order, decode as v2 -- what happens in each format?
3. Encode a v2 order, decode as v1 -- what happens?
4. Print the results for each format

### Payload Scaling Test

Benchmark with varying payload sizes:
- Small: 1 item, no metadata
- Medium: 10 items, 5 metadata entries
- Large: 100 items, 50 metadata entries
- XL: 1000 items, 200 metadata entries

Plot-friendly output (CSV or table) showing how each format scales.

## Success Criteria

1. All four benchmark functions compile and run with `go test -bench=. -benchmem`
2. The comparison table is generated with real measurements (not made up)
3. Protobuf is demonstrably smaller and faster than JSON
4. MessagePack falls between JSON and protobuf on most metrics
5. Schema evolution section shows concrete output for each format's behavior
6. Payload scaling section shows at least 4 data points per format
7. Round-trip correctness is verified for every format and size

## Constraints

- Benchmarks must use `testing.B` properly (not `time.Now` inside benchmark loops)
- Protobuf types must come from generated code, not hand-crafted structs
- Each format must encode the exact same logical data
- No benchmark contamination: reset timer after setup, run sub-benchmarks independently

## Guidance

- Start with JSON and protobuf since you have used them already.
- Add msgpack next -- its API is nearly identical to `encoding/json`.
- Structure your project:
  ```
  bench/
    proto/order.proto
    gen/order.pb.go
    order.go          (shared types + test fixture)
    bench_test.go     (all benchmarks)
    main.go           (comparison table + analysis)
  ```
- For the scaling test, write a `generateOrder(numItems, numMeta int) Order` factory.
- Use `b.Run("subtest", func(b *testing.B) { ... })` for sub-benchmarks.

## Stretch Goals

- Add `easyjson` (code-generated fast JSON) as a fourth format
- Add `flatbuffers` for zero-copy deserialization comparison
- Generate a visual chart (use `gonum/plot` or output CSV for external plotting)
- Test with concurrent marshal/unmarshal to see if any format has lock contention
- Profile with `pprof` and identify the hottest functions in each format
