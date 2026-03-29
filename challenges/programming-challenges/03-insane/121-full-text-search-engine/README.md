# 121. Full-Text Search Engine

```yaml
difficulty: insane
languages: [go, rust]
time_estimate: 50-80 hours
tags: [search-engine, inverted-index, bm25, compression, query-language, index-persistence, snippet-extraction]
bloom_level: [evaluate, create]
```

## Prerequisites

- Inverted index construction with positional data
- BM25 scoring and information retrieval fundamentals
- Binary encoding and variable-byte integer compression
- File I/O and memory-mapped files or buffered I/O
- Parser construction (recursive descent or Pratt parsing)
- Concurrency: parallel indexing and concurrent read access

## Learning Objectives

After completing this challenge you will be able to:

- **Create** a complete search engine with document ingestion, indexing, querying, and ranked retrieval
- **Implement** posting list compression using variable-byte encoding for space-efficient index storage
- **Design** a query language parser supporting Boolean operators, phrase queries, and field-based search
- **Build** snippet extraction with keyword highlighting from ranked search results
- **Evaluate** the engineering trade-offs between index build speed, query latency, index size, and update complexity

## The Challenge

Build a full-text search engine from scratch. Not a toy inverted index -- a complete system that ingests documents, builds compressed indexes, persists them to disk, ranks results using BM25, extracts relevant snippets with highlighted keywords, supports a query language with Boolean logic and phrase search, and handles incremental updates without rebuilding the entire index.

This is the core of what Lucene, Elasticsearch, and Tantivy do. Your implementation will be smaller but architecturally complete. Every component must work together: the tokenizer feeds the indexer, the indexer writes compressed posting lists, the query parser produces an execution plan, the executor traverses posting lists and scores documents, and the snippet extractor finds the best text fragments to display.

Both Go and Rust implementations are required. The Rust implementation should emphasize zero-copy deserialization and memory-mapped index files. The Go implementation should emphasize clean interfaces and goroutine-based parallel indexing.

## Requirements

1. **Document ingestion**: Accept documents as structs with an ID, title, body, and optional metadata fields. Tokenize each field independently. Store field-level term frequencies.

2. **Tokenizer pipeline**: Lowercase, split on word boundaries, remove configurable stop words, optionally apply stemming (Porter stemmer or simple suffix stripping). The pipeline must be pluggable.

3. **Inverted index**: Map each term to a posting list. Each posting contains: document ID, field ID, term frequency, and position list. Posting lists are sorted by document ID.

4. **Variable-byte encoding**: Compress posting lists using VByte encoding. Document IDs are stored as delta-encoded gaps (difference between consecutive IDs). Positions are also delta-encoded. Decode on the fly during query execution.

5. **BM25 ranking**: Score documents using Okapi BM25 with configurable k1 and b parameters. Field-specific boosts: title matches can be weighted higher than body matches.

6. **Query language**: Parse queries supporting:
   - Single terms: `database`
   - AND: `database AND index`
   - OR: `database OR search`
   - NOT: `database NOT sql`
   - Phrases: `"inverted index"`
   - Field-specific: `title:database`
   - Grouping: `(database OR search) AND index`
   - Implicit AND between adjacent terms: `database index` means `database AND index`

7. **Snippet extraction**: For each result, extract the most relevant text fragment (configurable length, default 200 chars) and wrap query terms in `<b>` tags. Select the fragment with the highest density of query terms.

8. **Index persistence**: Serialize the complete index to disk in a binary format. Separate files for: posting lists, term dictionary, document metadata, and corpus statistics. Load the index from disk without rebuilding.

9. **Incremental updates**: Support adding new documents to an existing index without full rebuild. New postings are appended. Periodically merge segments (like Lucene's segment merge) to maintain query performance.

10. **Concurrent access**: Readers must not block during index updates. Use a segment-based architecture where queries read from immutable segments while new documents write to a mutable buffer that is periodically flushed as a new segment.

## Hints

1. Start with in-memory indexing and querying working end-to-end before adding persistence or compression. Get BM25 ranking correct on a small corpus. Then add VByte encoding (verify round-trip correctness). Then add disk persistence. Incremental updates and segment merging come last.

2. VByte encoding: each byte stores 7 data bits and 1 continuation bit. The high bit indicates whether more bytes follow. To encode value 300: 300 in binary is `100101100`. Split into 7-bit groups: `0000010` `0101100`. Encode from least significant: `10101100` (continuation=1), `00000010` (continuation=0). Decoding reads bytes until the high bit is 0.

3. For the query parser, Pratt parsing (top-down operator precedence) handles the precedence of AND > OR naturally. Alternatively, use the shunting-yard algorithm to convert infix queries to postfix, then evaluate with a stack.

## Acceptance Criteria

- [ ] Documents with title and body fields are ingested and indexed correctly
- [ ] Tokenizer pipeline lowercases, splits, removes stop words, and the pipeline is configurable
- [ ] Inverted index stores correct per-field term frequencies and positions
- [ ] VByte encoding/decoding round-trips correctly for all values 0 through 2^28
- [ ] Delta encoding reduces posting list size by at least 50% compared to raw encoding
- [ ] BM25 scores match hand-calculated values for a 5-document test corpus
- [ ] Field boosts change ranking: title matches rank higher than body-only matches
- [ ] Query parser handles all specified syntax: AND, OR, NOT, phrases, fields, grouping
- [ ] Snippet extraction returns the highest-density fragment with highlighted terms
- [ ] Index persists to disk and loads back with identical query results
- [ ] Incremental document addition works without full rebuild
- [ ] Concurrent reads during index updates produce consistent (not corrupted) results
- [ ] End-to-end: index 1000+ documents, query, verify ranked results and snippets are correct

## Resources

- [Lucene in Action (Manning)](https://www.manning.com/books/lucene-in-action) -- architecture of the most widely deployed search library
- [Introduction to Information Retrieval (Manning, Raghavan, Schutze)](https://nlp.stanford.edu/IR-book/) -- free textbook, chapters 1-7 cover all the IR fundamentals
- [Tantivy source (Rust)](https://github.com/quickwit-oss/tantivy) -- production full-text search in Rust; study its segment architecture
- [Bleve source (Go)](https://github.com/blevesearch/bleve) -- full-text search in Go; study its index and analysis pipeline
- [Variable-Byte Encoding (Wikipedia)](https://en.wikipedia.org/wiki/Variable-length_quantity) -- compression scheme for posting lists
- [Robertson & Zaragoza: "BM25 and Beyond"](https://www.staff.city.ac.uk/~sbrp622/papers/foundations_bm25_review.pdf) -- the definitive BM25 reference
- [Zobel & Moffat: "Inverted Files for Text Search Engines" (2006)](https://people.eng.unimelb.edu.au/ammoffat/abstracts/zm06.html) -- comprehensive survey of inverted index engineering
