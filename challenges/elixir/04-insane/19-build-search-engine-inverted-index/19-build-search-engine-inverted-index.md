# 19. Build a Full-Text Search Engine with Ranking

**Difficulty**: Insane

## Prerequisites

- Mastered: ETS, binary pattern matching, string processing, recursive algorithms, GenServer
- Mastered: Sorting algorithms, merge operations, mathematical logarithms
- Familiarity with: Information retrieval theory (TF-IDF, BM25), Porter Stemmer algorithm, inverted index structure, positional indexes

## Problem Statement

Build a full-text search engine in Elixir from scratch — no external search libraries, no
Elasticsearch, no Solr. The engine must process text through an NLP-style pipeline, build
and maintain an inverted index, and rank results using BM25:

1. Implement a text processing pipeline: tokenization (split on whitespace and punctuation),
   lowercasing, stop word removal, and Porter stemming (or Snowball-compatible stemming).
   The pipeline must be composable: each stage is a function that transforms a token stream.
2. Implement an inverted index mapping each stemmed term to a posting list. Each posting
   is a `{doc_id, term_frequency, [position]}` tuple where `position` is the 0-based word
   offset in the document.
3. Implement TF-IDF scoring: compute term frequency (normalized) and inverse document
   frequency for each term in the query against all matching documents. Rank documents by
   their cosine similarity score against the query vector.
4. Implement BM25 scoring as an alternative to TF-IDF. BM25 uses parameters `k1` and `b`;
   your implementation must support configurable `k1` and `b` values (defaults: k1=1.5, b=0.75).
5. Implement boolean query execution: `AND`, `OR`, `NOT` operators over term sets. Boolean
   queries return document sets (unranked); combining boolean filtering with BM25 ranking
   is required.
6. Implement phrase queries: `"machine learning"` returns only documents containing those
   two tokens in adjacent positions, using the positional index.
7. Implement incremental indexing: documents can be added or removed from the index without
   rebuilding it from scratch. Removal must mark the document as deleted (tombstone) and
   exclude it from results.
8. Benchmark: index 1 million documents of average 500 words each; a keyword query must
   return the top-10 ranked results in under 50ms.

## Acceptance Criteria

- [ ] `SearchEngine.index(engine, doc_id, text)` processes the text through the pipeline
      and updates the inverted index; duplicate doc_id overwrites the previous entry.
- [ ] Tokenization correctly splits `"Hello, world! It's a test."` into
      `["hello", "world", "it's", "test"]` after stop word removal of `"a"`.
- [ ] Porter stemming reduces `"running"`, `"runs"`, `"ran"` to the same stem token;
      a query for `"run"` matches documents containing any of those forms.
- [ ] `SearchEngine.search(engine, "machine learning", scorer: :bm25, top_k: 10)` returns
      a list of `{doc_id, score}` tuples in descending score order.
- [ ] BM25 scores reflect document length normalization: a short document and a long document
      with the same term frequency rank the short document higher when `b > 0`.
- [ ] `SearchEngine.search(engine, "AI AND NOT robotics", boolean: true)` returns
      documents containing `"ai"` (after stemming) and excluding any document containing `"robotics"`.
- [ ] `SearchEngine.phrase_search(engine, "natural language processing")` returns only
      documents where all three tokens appear in consecutive positions.
- [ ] `SearchEngine.delete(engine, doc_id)` marks the document as deleted; it no longer
      appears in any subsequent search results.
- [ ] Benchmark: 1M documents indexed in under 30 minutes (wall clock); top-10 BM25 query
      completes in under 50ms for a 2-term query on a cold ETS table (no OS page cache warmup).

## What You Will Learn

- The inverted index data structure and why it enables sub-linear query time
- Porter stemmer algorithm: the five algorithmic phases and the suffix transformation rules
- TF-IDF vs BM25: why BM25 outperforms TF-IDF on variable-length documents and how the parameters control the length penalty
- Positional indexing: the additional storage cost of positions and why it enables phrase queries
- Boolean query execution via posting list intersection (AND), union (OR), and complement (NOT) using merge algorithms
- The incremental index update problem: posting list immutability vs deletion tombstones

## Hints

This exercise is intentionally sparse. Research:

- Store the inverted index in ETS as `{term, [{doc_id, tf, positions}]}` — use `:ets.lookup/2` for O(1) term access
- Porter stemmer: implement all five phases sequentially; each phase applies the first matching rule; the algorithm is deterministic for any English input
- BM25 formula: `score(d, q) = sum_t [ IDF(t) * (tf(t,d) * (k1+1)) / (tf(t,d) + k1 * (1 - b + b * len(d)/avg_len)) ]`
- Phrase query: after finding documents containing all terms, check that their positions satisfy `pos[term2] = pos[term1] + 1` for adjacent pairs
- For 1M documents, store the index in multiple ETS tables sharded by the first character of the term to reduce lock contention during concurrent indexing
- Stop words list: use the NLTK English stop words list (179 words) as the reference; hardcode it as a MapSet for O(1) lookup

## Reference Material

- "Introduction to Information Retrieval" — Manning, Raghavan & Schütze (Cambridge, 2008): available free online at https://nlp.stanford.edu/IR-book/
- Porter Stemmer algorithm paper: M.F. Porter, "An algorithm for suffix stripping", 1980
- BM25 paper: Robertson & Zaragoza, "The Probabilistic Relevance Framework: BM25 and Beyond", 2009
- Okapi BM25 Wikipedia article (for the formula and parameter guidance): https://en.wikipedia.org/wiki/Okapi_BM25

## Difficulty Rating

★★★★★★

## Estimated Time

50–70 hours
