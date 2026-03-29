# Solution: BM25 Search Ranking

## Architecture Overview

The system has four components:

1. **Tokenizer** -- splits and normalizes text into terms
2. **Index** -- inverted index storing term frequencies per document, plus corpus statistics (document lengths, average length, document count)
3. **BM25 Scorer** -- computes per-term and aggregate BM25 scores using corpus statistics
4. **TF-IDF Scorer** -- baseline scorer for comparison

```
Documents
    |
    v
Tokenizer -> Index (term -> [{doc_id, tf}], doc_lengths[], avgdl, N)
    |
    v
Query -> BM25 Scorer -> ranked [(doc_id, score)] -> Top-K
    |
    v
Query -> TF-IDF Scorer -> ranked [(doc_id, score)] -> Top-K (baseline)
```

Scoring iterates only over posting lists of query terms, not all documents. Per-document accumulators aggregate multi-term scores. A final sort by descending score produces the ranked result.

## Complete Solution (Go)

### Project Setup

```bash
mkdir -p bm25-search && cd bm25-search
go mod init bm25-search
```

### bm25.go

```go
package bm25

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

type DocID int

type Posting struct {
	DocID DocID
	TF    int
}

type ScoredDoc struct {
	DocID DocID
	Score float64
}

type Index struct {
	postings   map[string][]Posting
	docLengths map[DocID]int
	docCount   int
	avgDL      float64
}

type BM25Config struct {
	K1 float64
	B  float64
}

func DefaultBM25Config() BM25Config {
	return BM25Config{K1: 1.2, B: 0.75}
}

func NewIndex() *Index {
	return &Index{
		postings:   make(map[string][]Posting),
		docLengths: make(map[DocID]int),
	}
}

func tokenize(text string) []string {
	var tokens []string
	var current strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(unicode.ToLower(r))
		} else if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func (idx *Index) AddDocument(id DocID, text string) {
	tokens := tokenize(text)
	idx.docLengths[id] = len(tokens)

	termFreq := make(map[string]int)
	for _, t := range tokens {
		termFreq[t]++
	}

	for term, freq := range termFreq {
		idx.postings[term] = append(idx.postings[term], Posting{DocID: id, TF: freq})
	}

	idx.docCount++
	idx.recomputeAvgDL()
}

func (idx *Index) recomputeAvgDL() {
	total := 0
	for _, dl := range idx.docLengths {
		total += dl
	}
	if idx.docCount > 0 {
		idx.avgDL = float64(total) / float64(idx.docCount)
	}
}

func (idx *Index) idf(term string) float64 {
	n := len(idx.postings[term])
	N := float64(idx.docCount)
	nf := float64(n)
	return math.Log((N-nf+0.5)/(nf+0.5) + 1.0)
}

func (idx *Index) bm25TermScore(idf float64, tf int, dl int, cfg BM25Config) float64 {
	tfFloat := float64(tf)
	dlFloat := float64(dl)
	numerator := tfFloat * (cfg.K1 + 1.0)
	denominator := tfFloat + cfg.K1*(1.0-cfg.B+cfg.B*dlFloat/idx.avgDL)
	return idf * numerator / denominator
}

// SearchBM25 returns the top-K documents ranked by BM25 score.
func (idx *Index) SearchBM25(query string, k int, cfg BM25Config) []ScoredDoc {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}

	accumulators := make(map[DocID]float64)

	for _, term := range terms {
		idf := idx.idf(term)
		postings, ok := idx.postings[term]
		if !ok {
			continue
		}
		for _, p := range postings {
			dl := idx.docLengths[p.DocID]
			score := idx.bm25TermScore(idf, p.TF, dl, cfg)
			accumulators[p.DocID] += score
		}
	}

	results := make([]ScoredDoc, 0, len(accumulators))
	for docID, score := range accumulators {
		results = append(results, ScoredDoc{DocID: docID, Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if k > 0 && len(results) > k {
		results = results[:k]
	}
	return results
}

func (idx *Index) tfidfScore(tf int, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(tf) * math.Log(float64(idx.docCount)/float64(n))
}

// SearchTFIDF returns the top-K documents ranked by naive TF-IDF.
func (idx *Index) SearchTFIDF(query string, k int) []ScoredDoc {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}

	accumulators := make(map[DocID]float64)

	for _, term := range terms {
		postings, ok := idx.postings[term]
		if !ok {
			continue
		}
		n := len(postings)
		for _, p := range postings {
			score := idx.tfidfScore(p.TF, n)
			accumulators[p.DocID] += score
		}
	}

	results := make([]ScoredDoc, 0, len(accumulators))
	for docID, score := range accumulators {
		results = append(results, ScoredDoc{DocID: docID, Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if k > 0 && len(results) > k {
		results = results[:k]
	}
	return results
}
```

### bm25_test.go

```go
package bm25

import (
	"math"
	"testing"
)

func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

func buildTestCorpus() *Index {
	idx := NewIndex()
	idx.AddDocument(1, "the brown fox jumps over the brown dog")
	idx.AddDocument(2, "the lazy brown dog sat in the corner")
	idx.AddDocument(3, "the red fox bit the lazy dog")
	idx.AddDocument(4, "brown brown brown brown brown fox")
	idx.AddDocument(5, "quick systems for modern databases")
	return idx
}

func TestIDF(t *testing.T) {
	idx := buildTestCorpus()

	// "brown" appears in docs 1,2,4 -> n=3, N=5
	idfBrown := idx.idf("brown")
	expected := math.Log((5.0-3.0+0.5)/(3.0+0.5) + 1.0)
	if !almostEqual(idfBrown, expected, 1e-9) {
		t.Errorf("IDF(brown): expected %f, got %f", expected, idfBrown)
	}

	// "databases" appears in doc 5 -> n=1, N=5
	idfDb := idx.idf("databases")
	expectedDb := math.Log((5.0-1.0+0.5)/(1.0+0.5) + 1.0)
	if !almostEqual(idfDb, expectedDb, 1e-9) {
		t.Errorf("IDF(databases): expected %f, got %f", expectedDb, idfDb)
	}
}

func TestBM25SingleTerm(t *testing.T) {
	idx := buildTestCorpus()
	cfg := DefaultBM25Config()
	results := idx.SearchBM25("brown", 10, cfg)

	if len(results) == 0 {
		t.Fatal("expected results for 'brown'")
	}

	// Doc 4 has tf=5 for "brown" in a short doc, should rank high
	if results[0].DocID != 4 {
		t.Errorf("expected doc 4 to rank first, got doc %d", results[0].DocID)
	}
}

func TestBM25MultiTerm(t *testing.T) {
	idx := buildTestCorpus()
	cfg := DefaultBM25Config()
	results := idx.SearchBM25("brown fox", 10, cfg)

	if len(results) == 0 {
		t.Fatal("expected results for 'brown fox'")
	}
	// Scores should be sum of per-term BM25
	for _, r := range results {
		if r.Score <= 0 {
			t.Errorf("doc %d has non-positive score %f", r.DocID, r.Score)
		}
	}
}

func TestBM25UnknownTerm(t *testing.T) {
	idx := buildTestCorpus()
	cfg := DefaultBM25Config()
	results := idx.SearchBM25("xyznonexistent", 10, cfg)
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestBM25TopK(t *testing.T) {
	idx := buildTestCorpus()
	cfg := DefaultBM25Config()
	results := idx.SearchBM25("brown", 2, cfg)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestBM25ParameterSensitivity(t *testing.T) {
	idx := buildTestCorpus()

	highK1 := BM25Config{K1: 3.0, B: 0.75}
	lowK1 := BM25Config{K1: 0.5, B: 0.75}

	resultsHigh := idx.SearchBM25("brown", 10, highK1)
	resultsLow := idx.SearchBM25("brown", 10, lowK1)

	// Higher k1 should give higher absolute scores for high-tf documents
	if len(resultsHigh) == 0 || len(resultsLow) == 0 {
		t.Fatal("expected results")
	}

	// Doc 4 (tf=5) should have a bigger score gap with high k1
	var scoreHighK1, scoreLowK1 float64
	for _, r := range resultsHigh {
		if r.DocID == 4 {
			scoreHighK1 = r.Score
		}
	}
	for _, r := range resultsLow {
		if r.DocID == 4 {
			scoreLowK1 = r.Score
		}
	}
	if scoreHighK1 <= scoreLowK1 {
		t.Errorf("higher k1 should give higher score for high-tf doc: k1=3 -> %f, k1=0.5 -> %f",
			scoreHighK1, scoreLowK1)
	}
}

func TestBM25LengthNormalization(t *testing.T) {
	idx := buildTestCorpus()

	highB := BM25Config{K1: 1.2, B: 1.0}
	lowB := BM25Config{K1: 1.2, B: 0.0}

	resultsHighB := idx.SearchBM25("dog", 10, highB)
	resultsLowB := idx.SearchBM25("dog", 10, lowB)

	// With b=0, document length is ignored, so scores depend only on tf and IDF
	// With b=1, longer documents are penalized more
	if len(resultsHighB) == 0 || len(resultsLowB) == 0 {
		t.Fatal("expected results")
	}
	// Both should return results but with different orderings or scores
	if resultsHighB[0].Score == resultsLowB[0].Score {
		t.Error("different b values should produce different scores")
	}
}

func TestTFIDFBaseline(t *testing.T) {
	idx := buildTestCorpus()
	results := idx.SearchTFIDF("brown fox", 10)
	if len(results) == 0 {
		t.Fatal("expected TF-IDF results")
	}
}

func TestEmptyDocument(t *testing.T) {
	idx := NewIndex()
	idx.AddDocument(1, "")
	idx.AddDocument(2, "hello world")
	cfg := DefaultBM25Config()
	results := idx.SearchBM25("hello", 10, cfg)
	if len(results) != 1 || results[0].DocID != 2 {
		t.Error("empty document should not appear in results for 'hello'")
	}
}

func TestSingleDocumentCorpus(t *testing.T) {
	idx := NewIndex()
	idx.AddDocument(1, "the only document about search engines")
	cfg := DefaultBM25Config()
	results := idx.SearchBM25("search", 10, cfg)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}
```

### Run

```bash
go test -v ./...
```

## Complete Solution (Rust)

### Cargo.toml

```toml
[package]
name = "bm25-search"
version = "0.1.0"
edition = "2021"
```

### src/lib.rs

```rust
use std::collections::HashMap;

pub type DocID = u32;

#[derive(Debug, Clone)]
struct Posting {
    doc_id: DocID,
    tf: u32,
}

#[derive(Debug, Clone)]
pub struct ScoredDoc {
    pub doc_id: DocID,
    pub score: f64,
}

#[derive(Debug, Clone)]
pub struct BM25Config {
    pub k1: f64,
    pub b: f64,
}

impl Default for BM25Config {
    fn default() -> Self {
        Self { k1: 1.2, b: 0.75 }
    }
}

pub struct Index {
    postings: HashMap<String, Vec<Posting>>,
    doc_lengths: HashMap<DocID, u32>,
    doc_count: u32,
    avg_dl: f64,
    total_length: u64,
}

impl Index {
    pub fn new() -> Self {
        Self {
            postings: HashMap::new(),
            doc_lengths: HashMap::new(),
            doc_count: 0,
            avg_dl: 0.0,
            total_length: 0,
        }
    }

    pub fn add_document(&mut self, doc_id: DocID, text: &str) {
        let tokens = tokenize(text);
        let dl = tokens.len() as u32;
        self.doc_lengths.insert(doc_id, dl);

        let mut term_freq: HashMap<String, u32> = HashMap::new();
        for t in &tokens {
            *term_freq.entry(t.clone()).or_default() += 1;
        }

        for (term, freq) in term_freq {
            self.postings
                .entry(term)
                .or_default()
                .push(Posting { doc_id, tf: freq });
        }

        self.doc_count += 1;
        self.total_length += dl as u64;
        self.avg_dl = self.total_length as f64 / self.doc_count as f64;
    }

    fn idf(&self, term: &str) -> f64 {
        let n = self.postings.get(term).map(|p| p.len()).unwrap_or(0) as f64;
        let big_n = self.doc_count as f64;
        ((big_n - n + 0.5) / (n + 0.5) + 1.0).ln()
    }

    fn bm25_term_score(&self, idf: f64, tf: u32, dl: u32, cfg: &BM25Config) -> f64 {
        let tf_f = tf as f64;
        let dl_f = dl as f64;
        let numerator = tf_f * (cfg.k1 + 1.0);
        let denominator = tf_f + cfg.k1 * (1.0 - cfg.b + cfg.b * dl_f / self.avg_dl);
        idf * numerator / denominator
    }

    pub fn search_bm25(&self, query: &str, k: usize, cfg: &BM25Config) -> Vec<ScoredDoc> {
        let terms = tokenize(query);
        if terms.is_empty() {
            return vec![];
        }

        let mut accumulators: HashMap<DocID, f64> = HashMap::new();

        for term in &terms {
            let idf = self.idf(term);
            if let Some(postings) = self.postings.get(term) {
                for p in postings {
                    let dl = self.doc_lengths.get(&p.doc_id).copied().unwrap_or(0);
                    let score = self.bm25_term_score(idf, p.tf, dl, cfg);
                    *accumulators.entry(p.doc_id).or_default() += score;
                }
            }
        }

        let mut results: Vec<ScoredDoc> = accumulators
            .into_iter()
            .map(|(doc_id, score)| ScoredDoc { doc_id, score })
            .collect();

        results.sort_by(|a, b| b.score.partial_cmp(&a.score).unwrap_or(std::cmp::Ordering::Equal));

        if k > 0 && results.len() > k {
            results.truncate(k);
        }
        results
    }

    pub fn search_tfidf(&self, query: &str, k: usize) -> Vec<ScoredDoc> {
        let terms = tokenize(query);
        if terms.is_empty() {
            return vec![];
        }

        let mut accumulators: HashMap<DocID, f64> = HashMap::new();

        for term in &terms {
            if let Some(postings) = self.postings.get(term) {
                let n = postings.len() as f64;
                let big_n = self.doc_count as f64;
                for p in postings {
                    let score = p.tf as f64 * (big_n / n).ln();
                    *accumulators.entry(p.doc_id).or_default() += score;
                }
            }
        }

        let mut results: Vec<ScoredDoc> = accumulators
            .into_iter()
            .map(|(doc_id, score)| ScoredDoc { doc_id, score })
            .collect();

        results.sort_by(|a, b| b.score.partial_cmp(&a.score).unwrap_or(std::cmp::Ordering::Equal));

        if k > 0 && results.len() > k {
            results.truncate(k);
        }
        results
    }
}

fn tokenize(text: &str) -> Vec<String> {
    let mut tokens = Vec::new();
    let mut current = String::new();
    for ch in text.chars() {
        if ch.is_alphanumeric() {
            for c in ch.to_lowercase() {
                current.push(c);
            }
        } else if !current.is_empty() {
            tokens.push(std::mem::take(&mut current));
        }
    }
    if !current.is_empty() {
        tokens.push(current);
    }
    tokens
}

#[cfg(test)]
mod tests {
    use super::*;

    fn build_test_corpus() -> Index {
        let mut idx = Index::new();
        idx.add_document(1, "the brown fox jumps over the brown dog");
        idx.add_document(2, "the lazy brown dog sat in the corner");
        idx.add_document(3, "the red fox bit the lazy dog");
        idx.add_document(4, "brown brown brown brown brown fox");
        idx.add_document(5, "quick systems for modern databases");
        idx
    }

    #[test]
    fn test_idf() {
        let idx = build_test_corpus();
        let idf_brown = idx.idf("brown");
        let expected = ((5.0 - 3.0 + 0.5) / (3.0 + 0.5) + 1.0_f64).ln();
        assert!((idf_brown - expected).abs() < 1e-9);
    }

    #[test]
    fn test_bm25_single_term() {
        let idx = build_test_corpus();
        let cfg = BM25Config::default();
        let results = idx.search_bm25("brown", 10, &cfg);
        assert!(!results.is_empty());
        assert_eq!(results[0].doc_id, 4, "doc 4 has highest tf for 'brown'");
    }

    #[test]
    fn test_bm25_multi_term() {
        let idx = build_test_corpus();
        let cfg = BM25Config::default();
        let results = idx.search_bm25("brown fox", 10, &cfg);
        assert!(!results.is_empty());
        for r in &results {
            assert!(r.score > 0.0);
        }
    }

    #[test]
    fn test_bm25_unknown_term() {
        let idx = build_test_corpus();
        let cfg = BM25Config::default();
        let results = idx.search_bm25("xyznonexistent", 10, &cfg);
        assert!(results.is_empty());
    }

    #[test]
    fn test_bm25_top_k() {
        let idx = build_test_corpus();
        let cfg = BM25Config::default();
        let results = idx.search_bm25("brown", 2, &cfg);
        assert!(results.len() <= 2);
    }

    #[test]
    fn test_parameter_sensitivity_k1() {
        let idx = build_test_corpus();
        let high_k1 = BM25Config { k1: 3.0, b: 0.75 };
        let low_k1 = BM25Config { k1: 0.5, b: 0.75 };

        let results_high = idx.search_bm25("brown", 10, &high_k1);
        let results_low = idx.search_bm25("brown", 10, &low_k1);

        let score_high = results_high.iter().find(|r| r.doc_id == 4).unwrap().score;
        let score_low = results_low.iter().find(|r| r.doc_id == 4).unwrap().score;
        assert!(score_high > score_low, "higher k1 should increase scores for high-tf docs");
    }

    #[test]
    fn test_parameter_sensitivity_b() {
        let idx = build_test_corpus();
        let high_b = BM25Config { k1: 1.2, b: 1.0 };
        let low_b = BM25Config { k1: 1.2, b: 0.0 };

        let results_high = idx.search_bm25("dog", 10, &high_b);
        let results_low = idx.search_bm25("dog", 10, &low_b);

        assert_ne!(results_high[0].score, results_low[0].score);
    }

    #[test]
    fn test_tfidf_baseline() {
        let idx = build_test_corpus();
        let results = idx.search_tfidf("brown fox", 10);
        assert!(!results.is_empty());
    }

    #[test]
    fn test_empty_document() {
        let mut idx = Index::new();
        idx.add_document(1, "");
        idx.add_document(2, "hello world");
        let cfg = BM25Config::default();
        let results = idx.search_bm25("hello", 10, &cfg);
        assert_eq!(results.len(), 1);
        assert_eq!(results[0].doc_id, 2);
    }

    #[test]
    fn test_single_document_corpus() {
        let mut idx = Index::new();
        idx.add_document(1, "the only document about search engines");
        let cfg = BM25Config::default();
        let results = idx.search_bm25("search", 10, &cfg);
        assert_eq!(results.len(), 1);
    }
}
```

### Run and verify

```bash
# Go
cd bm25-search && go test -v ./...

# Rust
cd bm25-search && cargo test
```

Expected output (Rust):

```
running 10 tests
test tests::test_idf ... ok
test tests::test_bm25_single_term ... ok
test tests::test_bm25_multi_term ... ok
test tests::test_bm25_unknown_term ... ok
test tests::test_bm25_top_k ... ok
test tests::test_parameter_sensitivity_k1 ... ok
test tests::test_parameter_sensitivity_b ... ok
test tests::test_tfidf_baseline ... ok
test tests::test_empty_document ... ok
test tests::test_single_document_corpus ... ok

test result: ok. 10 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **IDF formula with `+ 1` inside the log**: The variant `ln((N - n + 0.5) / (n + 0.5) + 1)` ensures IDF is always non-negative. Without the `+ 1`, terms appearing in more than half the documents get negative IDF, which can cause counter-intuitive ranking inversions. This is the Robertson/Lucene variant used by Elasticsearch since version 5.

2. **Accumulator-based scoring**: Instead of computing full scores per document, we accumulate partial scores from each query term's posting list. This avoids iterating over documents that don't contain any query term. For sparse queries against large corpora, this is orders of magnitude faster than a full scan.

3. **Separate tokenizer, not integrated**: Tokenization is a pure function shared between indexing and querying. This ensures the same normalization rules apply to both sides. A mismatch (e.g., lowercasing at index time but not query time) silently breaks retrieval.

4. **No stop-word removal in this implementation**: BM25's IDF naturally down-weights common terms. A term appearing in all documents gets IDF close to 0, contributing almost nothing to the score. This makes explicit stop-word removal less critical for ranking (though still useful for index size reduction).

5. **f64 for all score arithmetic**: BM25 scores involve division and logarithms. Using f64 avoids precision issues that would appear with f32, especially when IDF values are close to zero for very common terms.

## Common Mistakes

1. **Using the wrong IDF variant**: The original BM25 IDF `log((N - n) / n)` goes negative for terms in more than half the documents. Always use the `+ 1` variant or clamp to zero.

2. **Forgetting to include document length in the index**: BM25 requires per-document token counts. If you only store term frequencies in the inverted index without recording total document length separately, you cannot compute the length normalization factor.

3. **Computing avgdl over document lengths excluding zeros**: Empty documents should contribute 0 to the average. Excluding them inflates avgdl and unfairly penalizes shorter-than-average documents.

4. **Integer division in score computation**: `int / int` truncates in both Go and Rust. Cast to f64 before dividing. This is especially dangerous in `dl / avgdl` where the result should be a fraction.

5. **Not handling query terms absent from the corpus**: If a query term has no posting list, its IDF is well-defined but there are no documents to score. Skip cleanly rather than panicking on a nil/None lookup.

## Performance Notes

- **Indexing**: O(D * T) where D is the number of documents and T is the average token count. Hash map insertions dominate.
- **Query scoring**: O(sum of posting list lengths for query terms). For a query with Q terms, this is O(Q * P_avg) where P_avg is the average posting list length. Much better than the naive O(N * Q) full scan.
- **Top-K extraction**: Sorting all accumulators is O(R log R) where R is the number of unique documents with non-zero scores. For large R, a min-heap of size K reduces this to O(R log K).
- **Memory**: The inverted index dominates. For a corpus of N documents with vocabulary V and average posting list length L, memory is O(V * L) for postings plus O(N) for document lengths. In practice, postings consume 8-12 bytes each (doc ID + tf).
- **BM25 vs TF-IDF speed**: Both have identical asymptotic complexity. BM25 has a higher constant factor per posting due to the division and length normalization, but the difference is negligible compared to I/O and cache effects.
