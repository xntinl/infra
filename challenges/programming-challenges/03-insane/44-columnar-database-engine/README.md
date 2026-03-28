# 44. Columnar Database Engine

<!--
difficulty: insane
category: databases
languages: [rust]
concepts: [columnar-storage, vectorized-execution, column-encoding, late-materialization, predicate-pushdown, query-processing, parquet-format]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [rust-ownership, generics-traits, binary-serialization, file-io, iterator-patterns, basic-sql-concepts]
-->

## Languages

- Rust (stable)

## Prerequisites

- Trait-based polymorphism and generic programming in Rust
- Binary serialization and deserialization of typed data
- Iterator composition and lazy evaluation patterns
- Familiarity with SQL semantics (SELECT, WHERE, GROUP BY, aggregation functions)
- Understanding of how row-oriented databases store and process data
- Basic knowledge of data compression concepts (run-length encoding, dictionary compression)

## The Challenge

Row-oriented databases store entire rows together: every column of row N lives next to every column of row N+1. This is efficient for transactional workloads that access complete rows. But analytical queries that scan millions of rows and only touch 3 of 50 columns waste enormous bandwidth reading data they will never use. A `SELECT AVG(salary) FROM employees WHERE department = 'engineering'` in a row store must read every byte of every column in every row, even though it only needs two columns.

Columnar databases invert the storage model. Each column is stored independently, contiguous in memory and on disk. A scan over a single column reads only that column's bytes. Columns of the same type compress dramatically: a million timestamps delta-encode into a fraction of their raw size, a low-cardinality status column run-length-encodes down to kilobytes. Compression is not just a space optimization -- it is a performance optimization because compressed data means less I/O and less memory bandwidth consumed.

Build a columnar storage engine in Rust. Store columns with type-specific encoding. Execute queries in a vectorized fashion, operating on batches of values rather than one row at a time. Support basic SQL operations: projection, filtering, grouping, and aggregation. Implement late materialization, where the engine operates on column indices as long as possible and only reconstructs full rows at the final output stage. Write data in a Parquet-like file format with row groups, column chunks, and per-chunk statistics that enable row group pruning.

This is how DuckDB, ClickHouse, Apache Parquet, and every modern analytics engine works. After this challenge, you will understand why columnar systems dominate analytical workloads, why encoding matters as much as indexing, why vectorized execution outperforms row-at-a-time Volcano-style iteration, and why the same query can be 100x faster on a columnar engine than on a row store.

## Acceptance Criteria

- [ ] Column storage with at least three encoding schemes: RLE for low-cardinality integer columns, dictionary encoding for string columns, delta encoding for sorted numeric columns, with fallback to plain/uncompressed encoding
- [ ] Automatic encoding selection based on column statistics: compute cardinality, sort order, and data type, then choose the encoding that offers the best compression
- [ ] Vectorized execution engine that processes batches of 1024+ values through each operator, avoiding per-row function call overhead
- [ ] SELECT with column projection: read only the requested columns from disk, never load columns that are not part of the query
- [ ] WHERE with predicate filters that produce selection vectors (lists of matching row indices) rather than copying matching rows
- [ ] WHERE with predicate filters operating directly on encoded data where possible (e.g., dictionary-encoded equality check compares dictionary indices instead of decoding strings)
- [ ] GROUP BY with aggregation functions: SUM, COUNT, AVG, MIN, MAX, all operating on column vectors
- [ ] Late materialization: filter and aggregate using selection vectors and column indices, reconstruct full rows only at the final output stage
- [ ] Predicate pushdown: push filter conditions below projection and aggregation operators so that filtering happens as early as possible in the pipeline
- [ ] Parquet-like file format with footer-based metadata: data organized into row groups, each row group contains column chunks, each chunk has encoding type and per-chunk statistics (min, max, null count)
- [ ] Row group pruning: skip entire row groups whose min/max statistics prove that no rows can match the query predicate
- [ ] Load and query a dataset of 1 million+ rows with at least 6 columns of mixed types (integers, floats, strings, timestamps)
- [ ] Demonstrate measurable compression ratio improvement for each encoding scheme over plain encoding
- [ ] Demonstrate measurable query speedup over naive row-at-a-time execution for scan-heavy analytical queries

## Strategic Nudges

1. Start with the storage layer: define your column chunk format, implement encoding/decoding for each scheme, and build the file writer/reader. Only then add the execution engine on top. If your storage layer is wrong, nothing above it will work.

2. For vectorized execution, define a `ColumnVector` type (a typed array of 1024 values plus a validity bitmap for nulls) as the unit of data flow between operators. Every operator takes one or more `ColumnVector` inputs and produces a `ColumnVector` output. This batch interface is what makes vectorized execution fast: it amortizes function call overhead and enables SIMD-friendly memory access patterns.

3. Late materialization means your WHERE operator produces a selection vector (a list of row indices that pass the filter), not filtered rows. Downstream operators use this selection vector to skip non-matching rows in every column without copying data. Only the final output stage gathers the selected rows into a result set.

## Going Further

- Implement a hash join operator for two-table joins using the build-probe pattern on column vectors
- Add SIMD-accelerated filter operations using Rust's `std::simd` (nightly) or manual intrinsics
- Implement zone maps: per-row-group min/max indices stored separately from data for fast pruning without reading chunk metadata
- Build a simple SQL parser that translates text queries into operator pipelines
- Implement morsel-driven parallelism: partition row groups across worker threads with work-stealing for multi-core query execution
- Add support for nullable columns with a separate validity bitmap alongside the encoded data
- Implement compiled query execution (generate native code for each query) and compare throughput against the vectorized interpreter

## Resources

- [CMU 15-721: Advanced Database Systems (Andy Pavlo)](https://15721.courses.cs.cmu.edu/spring2024/) -- graduate course on query execution, storage models, and vectorization
- [CMU 15-721: Vectorized Execution](https://15721.courses.cs.cmu.edu/spring2024/slides/06-vectorization.pdf) -- vectorized vs. compiled execution, batch processing
- [CMU 15-721: Storage Models & Data Layout](https://15721.courses.cs.cmu.edu/spring2024/slides/03-storage1.pdf) -- NSM vs. DSM, PAX, column encoding
- [Apache Parquet Format Specification](https://parquet.apache.org/docs/file-format/) -- row groups, column chunks, page encoding, statistics
- [MonetDB/X100: Hyper-Pipelining Query Execution (Boncz et al., 2005)](https://www.cidrdb.org/cidr2005/papers/P19.pdf) -- the paper that introduced vectorized execution
- [Column-Stores vs. Row-Stores (Abadi et al., 2008)](http://db.csail.mit.edu/projects/cstore/abadi-sigmod08.pdf) -- systematic comparison with late materialization analysis
- [DuckDB: An Embeddable Analytical Database](https://duckdb.org/why_duckdb.html) -- modern columnar engine design and architecture overview
- [The Design and Implementation of Modern Column-Oriented Database Systems (Abadi et al.)](https://stratos.seas.harvard.edu/files/stratos/files/columnstoresfntdbs.pdf) -- comprehensive survey of columnar techniques
- [Apache Arrow Columnar Format](https://arrow.apache.org/docs/format/Columnar.html) -- in-memory columnar format specification used by many analytics engines
- [Encoding Techniques in Apache Parquet](https://parquet.apache.org/docs/file-format/data-pages/encodings/) -- RLE, dictionary, delta, byte stream split encoding details
