# 85. BM25 Search Ranking

<!--
difficulty: advanced
category: search-engines-text-processing
languages: [go, rust]
concepts: [bm25, tf-idf, inverted-index, relevance-ranking, information-retrieval, document-scoring]
estimated_time: 10-14 hours
bloom_level: evaluate
prerequisites: [inverted-index, hash-maps, floating-point-arithmetic, sorting, file-io]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Inverted index construction (term-to-document mapping with frequencies)
- Hash maps and efficient lookup patterns
- Floating-point arithmetic and numerical stability awareness
- Sorting by computed scores
- Understanding of logarithms and their role in information theory

## Learning Objectives

- **Implement** the Okapi BM25 scoring function with all its components: term frequency saturation, inverse document frequency, and document length normalization
- **Analyze** how the parameters k1 and b control the behavior of BM25 and their impact on retrieval quality
- **Evaluate** ranking quality by comparing BM25 results against naive TF-IDF and term-count baselines
- **Design** a multi-term query scoring pipeline that combines per-term BM25 scores into a final document ranking
- **Apply** the inverted index as the foundation for efficient score computation without scanning every document

## The Challenge

Finding documents that contain a search term is easy. Ranking them by relevance is hard. A document mentioning "database" 50 times is not necessarily 50x more relevant than one mentioning it once -- the relationship between frequency and relevance saturates. A term appearing in every document carries less signal than a rare term. A short document achieving the same term count as a long one is likely more focused.

BM25 (Best Matching 25) captures all three insights in a single scoring function. It is the de facto standard ranking function in information retrieval, used by Elasticsearch, Lucene, and most production search systems. Despite its dominance, the formula is compact:

```
score(D, Q) = SUM over q in Q of:
    IDF(q) * (f(q,D) * (k1 + 1)) / (f(q,D) + k1 * (1 - b + b * |D| / avgdl))
```

Where `f(q,D)` is term frequency, `|D|` is document length, `avgdl` is average document length, and IDF is `log((N - n(q) + 0.5) / (n(q) + 0.5) + 1)`.

Build a search engine that indexes a corpus of documents, scores them using BM25 for multi-term queries, and returns results ranked by relevance. Implement the full pipeline: tokenization, index construction, IDF computation, per-document scoring, and ranked retrieval. Both Go and Rust implementations are required.

## Requirements

1. Implement document ingestion: tokenize text, build an inverted index storing term frequencies per document, and track document lengths (total token count per document)
2. Compute **IDF** for each term using the formula: `IDF(q) = ln((N - n(q) + 0.5) / (n(q) + 0.5) + 1)` where N is total documents and n(q) is the number of documents containing term q
3. Implement **BM25 scoring** per document for a single query term: `score = IDF(q) * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * dl / avgdl))` where tf is term frequency, dl is document length, avgdl is average document length
4. For multi-term queries, sum the per-term BM25 scores for each candidate document
5. Return the top-K documents sorted by descending BM25 score. Default K=10, configurable
6. Use default parameters k1=1.2 and b=0.75, with API to override both
7. Implement a baseline **naive TF-IDF** scorer for comparison: `score = tf * log(N / n(q))` without length normalization or saturation
8. Handle edge cases: query terms not in the corpus (IDF is well-defined but the term has no postings), empty documents, single-document corpus, all documents containing the query term
9. Provide a benchmark comparing BM25 vs TF-IDF ranking quality on a sample corpus (at least 20 documents) with at least 5 test queries
10. Write tests verifying: correct IDF values, correct BM25 scores for known inputs, correct ranking order, parameter sensitivity (varying k1 and b changes scores predictably)

## Hints

<details>
<summary>Hint 1: Corpus statistics you need</summary>

Before scoring, precompute:
- `N`: total number of documents
- `avgdl`: average document length (sum of all document lengths / N)
- Per-term: `n(q)` -- the number of documents containing term q (this is the posting list length)
- Per-document-term: `tf` -- the term frequency (stored in the inverted index)
- Per-document: `dl` -- the document length (total tokens, stored separately)

Store document lengths in a parallel array or map indexed by document ID. Computing `avgdl` is a single pass at index build time.
</details>

<details>
<summary>Hint 2: BM25 term frequency saturation</summary>

The numerator `tf * (k1 + 1)` and denominator `tf + k1 * (...)` create a saturation curve. As tf grows, the score approaches but never exceeds `(k1 + 1) * IDF`. This means the 50th occurrence of a term adds almost nothing compared to the 3rd occurrence.

The parameter k1 controls the saturation point. With k1=0, term frequency is ignored entirely (binary model). With k1=infinity, the score grows linearly with tf (like raw TF-IDF). The default k1=1.2 provides moderate saturation.
</details>

<details>
<summary>Hint 3: Document length normalization parameter b</summary>

The parameter b controls how much document length affects scoring. With b=0, document length is ignored (a 10,000-word document and a 100-word document with the same tf get the same score). With b=1, full length normalization is applied (long documents are penalized heavily).

The default b=0.75 provides a good balance: long documents get a mild penalty, but not so severe that they are never retrieved. For fields like titles (always short), use b closer to 0. For body text (variable length), b=0.75 works well.
</details>

<details>
<summary>Hint 4: Efficient scoring with posting lists</summary>

Do not iterate over all documents for each query term. Instead, iterate over the posting list for each query term (only documents containing that term). For each document in the posting list, compute the per-term BM25 contribution and add it to that document's accumulator. After processing all query terms, sort accumulators by score.

```
accumulators: HashMap<DocID, f64>

for term in query_terms:
    idf = compute_idf(term)
    for posting in index[term]:
        score = bm25_term_score(idf, posting.tf, doc_lengths[posting.doc_id], avgdl, k1, b)
        accumulators[posting.doc_id] += score

return top_k(accumulators, k)
```
</details>

<details>
<summary>Hint 5: Numerical precision with IDF</summary>

The IDF formula `ln((N - n + 0.5) / (n + 0.5) + 1)` is numerically stable: the `+ 1` inside the log ensures the result is always non-negative, even when n > N/2 (terms appearing in most documents). Some older BM25 formulations without the `+ 1` can produce negative IDF for very common terms, which distorts rankings.
</details>

## Acceptance Criteria

- [ ] Inverted index correctly stores term frequencies and document lengths for all ingested documents
- [ ] IDF values are correct for known inputs (verified against hand-calculated values)
- [ ] BM25 scores match hand-calculated values for a small test corpus (3-5 documents, 1-2 query terms)
- [ ] Multi-term queries combine per-term scores correctly (sum of individual BM25 contributions)
- [ ] Top-K results are sorted by descending score
- [ ] Varying k1 changes score saturation behavior predictably (higher k1 -> higher scores for high-tf documents)
- [ ] Varying b changes length normalization predictably (higher b -> lower scores for longer documents)
- [ ] Edge cases handled: unknown query terms, empty documents, single-document corpus
- [ ] Baseline TF-IDF scorer produces correct but differently-ranked results for comparison
- [ ] Benchmark demonstrates ranking differences between BM25 and TF-IDF on a non-trivial corpus
- [ ] All tests pass in both Go (`go test ./...`) and Rust (`cargo test`)

## Research Resources

- [Robertson & Zaragoza: "The Probabilistic Relevance Framework: BM25 and Beyond" (2009)](https://www.staff.city.ac.uk/~sbrp622/papers/foundations_bm25_review.pdf) -- the definitive BM25 reference by its creators
- [Introduction to Information Retrieval, Ch. 11: Probabilistic IR](https://nlp.stanford.edu/IR-book/html/htmledition/okapi-bm25-a-non-binary-model-1.html) -- Manning et al. textbook treatment of BM25
- [Elasticsearch: Similarity Module](https://www.elastic.co/guide/en/elasticsearch/reference/current/index-modules-similarity.html) -- how Elasticsearch implements BM25 with configurable parameters
- [Lucene BM25Similarity source](https://github.com/apache/lucene/blob/main/lucene/core/src/java/org/apache/lucene/search/similarities/BM25Similarity.java) -- production implementation in Lucene
- [Trotman et al.: "Improvements to BM25 and Language Models" (2014)](https://www.cs.otago.ac.nz/homepages/andrew/papers/2014-2.pdf) -- analysis of BM25 variants and parameter sensitivity
- [Wikipedia: Okapi BM25](https://en.wikipedia.org/wiki/Okapi_BM25) -- concise formula reference with derivation context
