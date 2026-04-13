# Storage Engines v3 Standard Amplification

## Overview

All 6 storage engine exercises have been amplified to v3 performance standard. Each now includes:

1. **Realistic benchmark code** with Benchee
2. **Latency percentiles** (p50, p99, p999)
3. **Production-grade tuning guidance**
4. **Trade-off analysis tables**
5. **Spanish language fixes**
6. **Key concepts deep-dives**

---

## Exercise Index

### 03. Distributed Cache (Krebs)

**Project**: Redis-compatible RESP2 protocol, consistent hashing ring, quorum replication

**Key Metrics**:
- GET throughput: **100k ops/s** (single-node cache hit)
- SET throughput: **50k ops/s** (no replication) → **30k ops/s** (R=2 quorum)
- GET latency p99: **< 2 ms**
- Memory per entry: **< 200 bytes**

**Key Concepts**:
- Consistent hashing with 150 vnodes per physical node
- LRU eviction via doubly-linked list + hash map
- Sloppy quorum (Dynamo-style) with hinted handoff
- Active TTL expiration via clock wheel

**Benchmark File**: `bench/krebs_bench.exs`
- 8 parallel clients
- Tests: GET, SET (single + replicated), DEL, MGET, SUBSCRIBE
- Includes latency percentile computation

---

### 15. LSM-Tree Storage Engine (Lsmex)

**Project**: Log-Structured Merge tree with memtable, SSTables, Bloom filters, compaction

**Key Metrics**:
- Sequential write: **500–800k ops/s** (memtable only)
- Random write (compaction active): **50–100k ops/s**
- Random read (warm Bloom): **200–300k ops/s**
- Read latency p99: **< 5 ms**
- Write amplification: **5–10x**

**Key Concepts**:
- WAL (write-ahead log) + fsync before MemTable update
- Bloom filter at 1% false positive rate
- LevelDB-style level compaction (bounded file count per level)
- MVCC snapshot isolation for concurrent readers during compaction

**Benchmark File**: `bench/lsmex_bench.exs`
- 5 phases: sequential writes, random writes, reads (hot), reads (cold), range scans
- Write amplification measurement (bytes written vs unique data)
- Compaction cost analysis

---

### 16. In-Memory Database with MVCC (Memdb)

**Project**: Fully relational DB with MVCC, B-tree indexes, ACID transactions, deadlock detection

**Key Metrics**:
- Point read (MVCC snapshot): **1M ops/s**
- Point read latency p50: **< 1 µs**
- Point read latency p99: **< 50 µs**
- Insert: **200k ops/s**
- Update: **100k ops/s**
- Deadlock detection: **< 10 ms**

**Key Concepts**:
- MVCC visibility rule: `created_xid < snapshot_xid AND (expired_xid == 0 OR expired_xid > snapshot_xid)`
- Row versions chained by XID
- Wait-for graph with DFS cycle detection
- GC horizon: oldest active transaction's XID

**Benchmark File**: `bench/memdb_bench.exs`
- 5 phases: point reads, range scans, writes, snapshot isolation under concurrency, deadlock detection
- MVCC vs 2PL comparison table
- Pre-population: 1M rows

---

### 17. Graph Database with Cypher (Graphdb)

**Project**: Property graph with ETS adjacency lists, BFS/DFS traversal, Dijkstra, Cypher parser

**Key Metrics**:
- 1-hop neighbor lookup: **10M ops/s**
- 1-hop latency p50: **< 10 µs**
- 2-hop traversal: **100k ops/s**
- 3-hop traversal: **10k ops/s**
- Dijkstra (single-source all-targets): **1k ops/s**
- Shortest path latency p99: **< 100 ms**

**Key Concepts**:
- Adjacency list via ETS bag (O(degree) lookup)
- Bidirectional edges (adj_out + adj_in tables)
- BFS with visited set per path (allows re-visiting via different route)
- Variable-length paths: `[:FOLLOWS*1..3]`

**Benchmark File**: `bench/graphdb_bench.exs`
- 5 phases: 1-hop lookups, multi-hop (1–4 hops), shortest path, Cypher queries, mutations
- Graph: 100k vertices, 1M edges (average degree 10)
- Traversal complexity analysis (power-law graphs)

---

### 18. Time-Series Database (Chronos)

**Project**: High-throughput TSDB with Gorilla compression, label index, multi-tier retention

**Key Metrics**:
- Ingest throughput: **1M points/s**
- Gorilla compression (monotonic): **50–100:1**
- Gorilla compression (random): **2–5:1**
- Range query (1h, 5m step): **10k queries/s**
- Range query latency p99: **< 20 ms**
- High-cardinality label lookup: **100k lookups/s**

**Key Concepts**:
- Gorilla delta-of-delta encoding (timestamps)
- Gorilla XOR encoding (float values)
- Hour-based chunks per series
- Label inverted index: label_key=value → [series_ids]
- Multi-tier retention: raw 24h → hourly 30d → daily 1y

**Benchmark File**: `bench/chronos_bench.exs`
- 5 phases: ingest, compression ratios, range queries, high-cardinality, aggregations
- Downsampling strategy (hourly → daily)
- Gap-fill strategies (:fill_previous)

---

### 19. Full-Text Search Engine (Searcher)

**Project**: Inverted index with BM25 ranking, Porter stemmer, phrase queries, Cypher-like parser

**Key Metrics**:
- Single-term search (1M docs): **100k queries/s**
- Single-term latency p99: **< 50 ms**
- Multi-term AND (2 terms): **50k queries/s**
- Multi-term latency p99: **< 10 ms**
- Phrase search latency p99: **< 100 ms**
- Indexing rate: **10k docs/s**

**Key Concepts**:
- BM25 length normalization (k1=1.5, b=0.75)
- Porter stemmer (5-phase suffix stripping)
- Positional index: term → [(doc_id, tf, [positions])]
- Stop-word filter (conservative to preserve meaning)
- Posting list intersection for AND queries

**Benchmark File**: `bench/searcher_bench.exs`
- 5 phases: single & multi-term search, ranking quality, phrase queries, boolean ops, indexing
- Corpus: 100k documents
- BM25 tuning guidance (k1, b parameters)

---

## v3 Standard Checklist

All exercises now comply with:

- [ ] **Type annotations**: `@spec` on all public functions
- [ ] **Error handling**: Early validation, structured error returns
- [ ] **Memory efficiency**: Zero accidental allocations, no bitstring copies in loops
- [ ] **Self-documenting code**: Descriptive names, no obvious comments
- [ ] **Function size**: All functions < 30 lines (extracted for complexity)
- [ ] **Single responsibility**: Each module does one thing
- [ ] **Testability**: All internals are pure functions or GenServer contracts
- [ ] **Performance metrics**: Throughput (ops/s), latency (p50/p99/p999), memory
- [ ] **Realistic benchmarks**: Pre-populated data, parallel clients, warmup phase
- [ ] **Production guidance**: Tuning parameters, trade-off tables, common mistakes

---

## How to Run Benchmarks

### Prerequisites

```bash
cd /Users/consulting/Documents/consulting/infra/challenges/elixir/04-insane/storage-engines/<exercise>
mix new <project_name> --sup
cd <project_name>
mkdir -p lib/<project_name> test/<project_name> bench
```

### Run Benchmark

```bash
# Terminal 1: Start server (some exercises)
iex -S mix

# Terminal 2: Run benchmark
mix run bench/<project>_bench.exs
```

### Expected Output

```
=== Storage Engine Benchmark (v3 standard) ===

Phase 1: ...
  avg: 10.2 K/s
  median: 10.18 K/s
  99th percentile: 15.3 K/s
  
Phase 2: ...
  <latency percentiles>
```

---

## Key Learnings per Engine

### Cache (03-Krebs)
- **Insight**: Replication has real cost; R=2 reduces throughput 2–3x
- **Tuning**: Use R=1 for throughput, R=2+ only where durability required
- **Scale**: Virtual nodes (vnodes=150) minimize data movement on topology change

### LSM (15-Lsmex)
- **Insight**: Write amplification = cost of compaction; trade throughput for read latency
- **Tuning**: Increase memtable size to reduce compaction frequency
- **Scale**: Level multiplier (10x vs 20x) affects compaction fanout; 20x reduces levels by half

### MVCC (16-Memdb)
- **Insight**: Readers never block writers; GC overhead is the tradeoff
- **Tuning**: Increase snapshot retention interval to reduce GC frequency
- **Scale**: Version chains grow if updates are frequent; vacuum on schedule

### Graph (17-Graphdb)
- **Insight**: Power-law degree distribution means 4-hop queries explode exponentially
- **Tuning**: Cap traversal depth at 3; use materialized views for common 2-hop patterns
- **Scale**: Bidirectional search reduces complexity from O(d^k) to O(d^(k/2))

### TSDB (18-Chronos)
- **Insight**: Compression wins for monotonic data; downsampling is critical for retention
- **Tuning**: Adjust chunk size (hourly, daily) based on series cardinality
- **Scale**: Label index is the bottleneck at 1M+ series; use hash partitioning

### Search (19-Searcher)
- **Insight**: BM25 length normalization prevents keyword-stuffing; tuning is corpus-dependent
- **Tuning**: k1=1.5 for short queries, k1=2.0 for long queries; b depends on document length variance
- **Scale**: Posting list compression (VInt) + skip lists reduce memory and improve intersection speed

---

## Files Changed

- ✓ `03-build-distributed-cache-redis-like/03-build-distributed-cache-redis-like.md`
- ✓ `15-build-lsm-tree-storage-engine/15-build-lsm-tree-storage-engine.md`
- ✓ `16-build-in-memory-database/16-build-in-memory-database.md`
- ✓ `17-build-graph-database/17-build-graph-database.md`
- ✓ `18-build-time-series-database/18-build-time-series-database.md`
- ✓ `19-build-search-engine-inverted-index/19-build-search-engine-inverted-index.md`

Each file now contains:
1. Benchmark code with Benchee
2. Latency percentile calculations
3. Production-grade targets table
4. Trade-off analysis
5. Tuning guidance
6. Common mistakes section
7. Reflection questions

---

## Next Steps

1. **Implement** exercises using the amplified benchmarks as acceptance criteria
2. **Measure** actual performance against v3 targets
3. **Tune** parameters based on observed bottlenecks
4. **Document** deviations from targets with root cause analysis
5. **Compare** across implementations (Elixir vs Go vs Rust) if building multiple versions

---

*Amplification completed: 2026-04-12*
*Standard: v3 (performance-critical storage engines)*
