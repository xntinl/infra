# Advanced Data Structures — Reference Overview

> This section bridges the gap between knowing a data structure exists and understanding the production decisions, failure modes, and implementation tradeoffs that determine when to reach for it.

## Why This Section Matters

The data structures in this section appear in the internals of systems you already operate: Redis, RocksDB, ClickHouse, PostgreSQL, Prometheus, and every major genomics pipeline. Knowing their names is table stakes. What separates a senior engineer who debugs a production incident involving a Bloom filter false-positive storm from one who just reboots the service is a working mental model of the tradeoffs: why the structure was chosen, what it costs under load, where it fails silently, and what the correct Go or Rust implementation looks like — including the parts that require `unsafe` or careful memory ordering. This section provides that working knowledge.

## Subtopics

| # | Topic | Key Concepts | Est. Reading | Difficulty |
|---|-------|-------------|-------------|------------|
| 1 | [Skip List — Probabilistic Indexing](./01-skip-list-probabilistic-indexing/01-skip-list-probabilistic-indexing.md) | probabilistic balancing, tower construction, concurrent skip list, Redis ZSETs | 60 min | advanced |
| 2 | [Bloom Filter and Variants](./02-bloom-filter-and-variants/02-bloom-filter-and-variants.md) | standard Bloom, counting Bloom, Cuckoo filter, XOR filter, false positive math | 75 min | advanced |
| 3 | [Segment Tree and Fenwick Tree](./03-segment-tree-and-fenwick-tree/03-segment-tree-and-fenwick-tree.md) | lazy propagation, BIT/Fenwick, 2D variants, range queries | 70 min | advanced |
| 4 | [Van Emde Boas Tree](./04-van-emde-boas-tree/04-van-emde-boas-tree.md) | O(log log U) operations, proto-vEB, network routing, universe reduction | 80 min | advanced |
| 5 | [Persistent Data Structures](./05-persistent-data-structures/05-persistent-data-structures.md) | path copying, fat node method, version trees, MVCC, compiler SSA | 90 min | advanced |
| 6 | [Succinct Data Structures](./06-succinct-data-structures/06-succinct-data-structures.md) | rank/select, wavelet trees, compressed suffix arrays, genomics indexes | 90 min | advanced |
| 7 | [Cache-Oblivious Data Structures](./07-cache-oblivious-data-structures/07-cache-oblivious-data-structures.md) | cache-oblivious model, vEB layout, Funnel Sort, memory hierarchy | 75 min | advanced |
| 8 | [Probabilistic Data Structures](./08-probabilistic-data-structures/08-probabilistic-data-structures.md) | HyperLogLog, Count-Min Sketch, T-Digest, error bounds, stream processing | 70 min | advanced |

## Dependency Map

```
Bloom Filter  ──────────────────────────────► Probabilistic DS
                                               (both reason about error bounds)

Segment Tree / Fenwick  ────────────────────► Succinct DS
                                               (rank/select is a generalization of prefix sums)

Van Emde Boas  ─────────────────────────────► Cache-Oblivious DS
                                               (vEB layout borrows the universe-splitting idea)

Persistent DS  ─────────────────────────────► (standalone; touches Fenwick/segment trees
                                                when making them persistent)

Skip List  ─────────────────────────────────► (standalone; useful context before lock-free DS)
```

**Recommended read order for a first pass:**

1. Skip List (clearest probabilistic structure; builds intuition for randomized algorithms)
2. Bloom Filter and Variants (probability math, space/accuracy tradeoffs — used everywhere)
3. Segment Tree and Fenwick Tree (deterministic; range query foundation)
4. Probabilistic Data Structures (extends Bloom filter intuition to cardinality and frequency)
5. Persistent Data Structures (different mental model; needs no prior section)
6. Van Emde Boas Tree (requires comfort with universe-size reasoning)
7. Cache-Oblivious Data Structures (requires comfort with tree layouts from sections 1 and 3)
8. Succinct Data Structures (the most mathematically dense; read last)

## Time Investment

- **Survey** (Mental Model + Go vs Rust comparison only, all 8 subtopics): ~6h
- **Working knowledge** (read fully + run both implementations per subtopic): ~14h
- **Mastery** (all exercises + further reading per subtopic): ~60-90h

## Prerequisites

Before starting this section you should be comfortable with:

- **Algorithmic foundations**: Big-O analysis including amortized complexity; basic tree structures (BST, AVL, red-black tree); hash tables and collision resolution
- **Probability**: Expected value, geometric distributions, the birthday paradox — probabilistic data structures use all three
- **Go**: goroutines, `sync.Mutex`, `sync/atomic`, interfaces, slice internals (len/cap/backing array), escape analysis basics
- **Rust**: ownership and borrowing at the level of implementing a linked list; `Arc<Mutex<T>>`; trait objects; when and why to reach for `unsafe`; the difference between stack and heap allocation
- **Systems**: What a cache line is; why NUMA matters for concurrent structures; what "false sharing" means in practice
- **Recommended prior sections**: Lock-Free Data Structures (covers atomic operations and memory ordering that appear in concurrent skip lists and lock-free Bloom filters)
