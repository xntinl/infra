# Solution: Inverted Index Text Search

## Architecture Overview

The system has three layers:

1. **Tokenizer** -- splits raw text into normalized tokens, applies stop-word filtering
2. **Index** -- maintains the inverted index mapping terms to posting lists with positional data
3. **Query Evaluator** -- parses query strings and evaluates them against the index using set operations on posting lists

```
Document text
    |
    v
Tokenizer (split, lowercase, filter stop words)
    |
    v
Index (term -> [{doc_id, freq, [positions]}])
    |
    v
Query Evaluator (AND/OR/NOT/PHRASE -> set operations on posting lists)
    |
    v
Result: sorted list of matching document IDs
```

Posting lists are kept sorted by document ID, enabling O(n+m) merge operations for Boolean queries. Phrase queries perform a second pass over position lists to verify term adjacency.

## Complete Solution (Go)

### Project Setup

```bash
mkdir -p inverted-index && cd inverted-index
go mod init inverted-index
```

### index.go

```go
package invertedindex

import (
	"sort"
	"strings"
	"unicode"
)

type DocID int

type Posting struct {
	DocID     DocID
	Frequency int
	Positions []int
}

type InvertedIndex struct {
	index     map[string][]Posting
	stopWords map[string]bool
	docCount  int
}

var defaultStopWords = []string{
	"the", "a", "an", "is", "are", "was", "in", "of",
	"to", "and", "or", "for", "on", "it", "at",
}

func NewInvertedIndex(extraStopWords []string) *InvertedIndex {
	sw := make(map[string]bool)
	for _, w := range defaultStopWords {
		sw[w] = true
	}
	for _, w := range extraStopWords {
		sw[strings.ToLower(w)] = true
	}
	return &InvertedIndex{
		index:     make(map[string][]Posting),
		stopWords: sw,
	}
}

func (idx *InvertedIndex) tokenize(text string) []string {
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(unicode.ToLower(r))
		} else {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func (idx *InvertedIndex) filterStopWords(tokens []string) []string {
	filtered := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if !idx.stopWords[t] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (idx *InvertedIndex) AddDocument(id DocID, text string) {
	tokens := idx.tokenize(text)

	// Build position map before stop-word removal for accurate positions
	termPositions := make(map[string][]int)
	for pos, token := range tokens {
		if !idx.stopWords[token] {
			termPositions[token] = append(termPositions[token], pos)
		}
	}

	for term, positions := range termPositions {
		posting := Posting{
			DocID:     id,
			Frequency: len(positions),
			Positions: positions,
		}
		idx.index[term] = append(idx.index[term], posting)
	}
	idx.docCount++
}

func (idx *InvertedIndex) getPostings(term string) []Posting {
	normalized := strings.ToLower(term)
	postings, ok := idx.index[normalized]
	if !ok {
		return nil
	}
	return postings
}

func (idx *InvertedIndex) allDocIDs() []DocID {
	seen := make(map[DocID]bool)
	for _, postings := range idx.index {
		for _, p := range postings {
			seen[p.DocID] = true
		}
	}
	result := make([]DocID, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func intersectDocIDs(a, b []DocID) []DocID {
	var result []DocID
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

func unionDocIDs(a, b []DocID) []DocID {
	var result []DocID
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			result = append(result, a[i])
			i++
		} else {
			result = append(result, b[j])
			j++
		}
	}
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)
	return result
}

func differenceDocIDs(a, b []DocID) []DocID {
	exclude := make(map[DocID]bool, len(b))
	for _, id := range b {
		exclude[id] = true
	}
	var result []DocID
	for _, id := range a {
		if !exclude[id] {
			result = append(result, id)
		}
	}
	return result
}

func postingsToDocIDs(postings []Posting) []DocID {
	ids := make([]DocID, len(postings))
	for i, p := range postings {
		ids[i] = p.DocID
	}
	return ids
}

// SearchAND returns documents containing all given terms.
func (idx *InvertedIndex) SearchAND(terms []string) []DocID {
	if len(terms) == 0 {
		return nil
	}
	result := postingsToDocIDs(idx.getPostings(terms[0]))
	for _, term := range terms[1:] {
		result = intersectDocIDs(result, postingsToDocIDs(idx.getPostings(term)))
	}
	return result
}

// SearchOR returns documents containing any of the given terms.
func (idx *InvertedIndex) SearchOR(terms []string) []DocID {
	var result []DocID
	for _, term := range terms {
		result = unionDocIDs(result, postingsToDocIDs(idx.getPostings(term)))
	}
	return result
}

// SearchNOT excludes documents matching excludeTerm from the base set.
func (idx *InvertedIndex) SearchNOT(base []DocID, excludeTerm string) []DocID {
	return differenceDocIDs(base, postingsToDocIDs(idx.getPostings(excludeTerm)))
}

// SearchPhrase returns documents where terms appear consecutively.
func (idx *InvertedIndex) SearchPhrase(terms []string) []DocID {
	if len(terms) == 0 {
		return nil
	}

	// Find documents containing all terms
	postingsPerTerm := make([][]Posting, len(terms))
	for i, term := range terms {
		normalized := strings.ToLower(term)
		postingsPerTerm[i] = idx.getPostings(normalized)
		if len(postingsPerTerm[i]) == 0 {
			return nil
		}
	}

	candidateDocs := postingsToDocIDs(postingsPerTerm[0])
	for _, postings := range postingsPerTerm[1:] {
		candidateDocs = intersectDocIDs(candidateDocs, postingsToDocIDs(postings))
	}

	// For each candidate, verify position adjacency
	var result []DocID
	for _, docID := range candidateDocs {
		if idx.verifyPhrase(docID, postingsPerTerm) {
			result = append(result, docID)
		}
	}
	return result
}

func (idx *InvertedIndex) verifyPhrase(docID DocID, postingsPerTerm [][]Posting) bool {
	positionSets := make([][]int, len(postingsPerTerm))
	for i, postings := range postingsPerTerm {
		for _, p := range postings {
			if p.DocID == docID {
				positionSets[i] = p.Positions
				break
			}
		}
		if positionSets[i] == nil {
			return false
		}
	}

	// Check if there exists a starting position in term[0]
	// such that term[i] appears at startPos+i for all i
	for _, startPos := range positionSets[0] {
		found := true
		for i := 1; i < len(positionSets); i++ {
			target := startPos + i
			if !containsPosition(positionSets[i], target) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

func containsPosition(positions []int, target int) bool {
	idx := sort.SearchInts(positions, target)
	return idx < len(positions) && positions[idx] == target
}
```

### index_test.go

```go
package invertedindex

import (
	"testing"
)

func setupTestIndex() *InvertedIndex {
	idx := NewInvertedIndex(nil)
	idx.AddDocument(1, "The quick brown fox jumps over the lazy dog")
	idx.AddDocument(2, "A fast brown cat sits on the lazy mat")
	idx.AddDocument(3, "Quick systems design for distributed databases")
	idx.AddDocument(4, "The dog chased the cat across the yard")
	return idx
}

func TestANDQuery(t *testing.T) {
	idx := setupTestIndex()
	result := idx.SearchAND([]string{"brown", "lazy"})
	expected := []DocID{1, 2}
	assertDocIDs(t, expected, result)
}

func TestORQuery(t *testing.T) {
	idx := setupTestIndex()
	result := idx.SearchOR([]string{"dog", "cat"})
	expected := []DocID{1, 2, 4}
	assertDocIDs(t, expected, result)
}

func TestNOTQuery(t *testing.T) {
	idx := setupTestIndex()
	base := idx.SearchOR([]string{"brown", "dog"})
	result := idx.SearchNOT(base, "cat")
	expected := []DocID{1}
	assertDocIDs(t, expected, result)
}

func TestPhraseQuery(t *testing.T) {
	idx := setupTestIndex()
	result := idx.SearchPhrase([]string{"brown", "fox"})
	expected := []DocID{1}
	assertDocIDs(t, expected, result)
}

func TestPhraseQueryNoMatch(t *testing.T) {
	idx := setupTestIndex()
	result := idx.SearchPhrase([]string{"fox", "brown"}) // wrong order
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestEmptyResult(t *testing.T) {
	idx := setupTestIndex()
	result := idx.SearchAND([]string{"nonexistent"})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestStopWordsFiltered(t *testing.T) {
	idx := setupTestIndex()
	result := idx.getPostings("the")
	if len(result) != 0 {
		t.Error("stop word 'the' should not be in index")
	}
}

func TestUnicodeTokenization(t *testing.T) {
	idx := NewInvertedIndex(nil)
	idx.AddDocument(1, "El cafe esta caliente")
	idx.AddDocument(2, "Busqueda rapida y eficiente")
	result := idx.SearchAND([]string{"cafe"})
	expected := []DocID{1}
	assertDocIDs(t, expected, result)
}

func TestRepeatedTerms(t *testing.T) {
	idx := NewInvertedIndex(nil)
	idx.AddDocument(1, "search search search engine")
	postings := idx.getPostings("search")
	if len(postings) != 1 {
		t.Fatalf("expected 1 posting, got %d", len(postings))
	}
	if postings[0].Frequency != 3 {
		t.Errorf("expected frequency 3, got %d", postings[0].Frequency)
	}
}

func assertDocIDs(t *testing.T, expected, actual []DocID) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Errorf("expected %v, got %v", expected, actual)
		return
	}
	for i := range expected {
		if expected[i] != actual[i] {
			t.Errorf("expected %v, got %v", expected, actual)
			return
		}
	}
}
```

### Run and verify

```bash
cd inverted-index
go test -v ./...
```

Expected output:

```
=== RUN   TestANDQuery
--- PASS: TestANDQuery (0.00s)
=== RUN   TestORQuery
--- PASS: TestORQuery (0.00s)
=== RUN   TestNOTQuery
--- PASS: TestNOTQuery (0.00s)
=== RUN   TestPhraseQuery
--- PASS: TestPhraseQuery (0.00s)
=== RUN   TestPhraseQueryNoMatch
--- PASS: TestPhraseQueryNoMatch (0.00s)
=== RUN   TestEmptyResult
--- PASS: TestEmptyResult (0.00s)
=== RUN   TestStopWordsFiltered
--- PASS: TestStopWordsFiltered (0.00s)
=== RUN   TestUnicodeTokenization
--- PASS: TestUnicodeTokenization (0.00s)
=== RUN   TestRepeatedTerms
--- PASS: TestRepeatedTerms (0.00s)
PASS
```

## Complete Solution (Rust)

### Cargo.toml

```toml
[package]
name = "inverted-index"
version = "0.1.0"
edition = "2021"
```

### src/lib.rs

```rust
use std::collections::{BTreeMap, BTreeSet, HashMap, HashSet};

pub type DocID = u32;

#[derive(Debug, Clone)]
pub struct Posting {
    pub doc_id: DocID,
    pub frequency: u32,
    pub positions: Vec<u32>,
}

pub struct InvertedIndex {
    index: BTreeMap<String, Vec<Posting>>,
    stop_words: HashSet<String>,
    doc_count: u32,
}

const DEFAULT_STOP_WORDS: &[&str] = &[
    "the", "a", "an", "is", "are", "was", "in", "of",
    "to", "and", "or", "for", "on", "it", "at",
];

impl InvertedIndex {
    pub fn new(extra_stop_words: &[&str]) -> Self {
        let mut stop_words: HashSet<String> = DEFAULT_STOP_WORDS
            .iter()
            .map(|s| s.to_string())
            .collect();
        for w in extra_stop_words {
            stop_words.insert(w.to_lowercase());
        }
        Self {
            index: BTreeMap::new(),
            stop_words,
            doc_count: 0,
        }
    }

    fn tokenize(&self, text: &str) -> Vec<String> {
        let mut tokens = Vec::new();
        let mut current = String::new();

        for ch in text.chars() {
            if ch.is_alphanumeric() {
                for lower in ch.to_lowercase() {
                    current.push(lower);
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

    pub fn add_document(&mut self, doc_id: DocID, text: &str) {
        let tokens = self.tokenize(text);

        let mut term_positions: HashMap<String, Vec<u32>> = HashMap::new();
        for (pos, token) in tokens.iter().enumerate() {
            if !self.stop_words.contains(token) {
                term_positions
                    .entry(token.clone())
                    .or_default()
                    .push(pos as u32);
            }
        }

        for (term, positions) in term_positions {
            let posting = Posting {
                doc_id,
                frequency: positions.len() as u32,
                positions,
            };
            self.index.entry(term).or_default().push(posting);
        }
        self.doc_count += 1;
    }

    fn get_postings(&self, term: &str) -> &[Posting] {
        let normalized = term.to_lowercase();
        self.index.get(&normalized).map(|v| v.as_slice()).unwrap_or(&[])
    }

    fn postings_to_doc_ids(postings: &[Posting]) -> Vec<DocID> {
        postings.iter().map(|p| p.doc_id).collect()
    }

    pub fn search_and(&self, terms: &[&str]) -> Vec<DocID> {
        if terms.is_empty() {
            return vec![];
        }
        let mut result = Self::postings_to_doc_ids(self.get_postings(terms[0]));
        for term in &terms[1..] {
            let other = Self::postings_to_doc_ids(self.get_postings(term));
            result = intersect(&result, &other);
        }
        result
    }

    pub fn search_or(&self, terms: &[&str]) -> Vec<DocID> {
        let mut result = vec![];
        for term in terms {
            let other = Self::postings_to_doc_ids(self.get_postings(term));
            result = union(&result, &other);
        }
        result
    }

    pub fn search_not(&self, base: &[DocID], exclude_term: &str) -> Vec<DocID> {
        let exclude: BTreeSet<DocID> = Self::postings_to_doc_ids(self.get_postings(exclude_term))
            .into_iter()
            .collect();
        base.iter().filter(|id| !exclude.contains(id)).copied().collect()
    }

    pub fn search_phrase(&self, terms: &[&str]) -> Vec<DocID> {
        if terms.is_empty() {
            return vec![];
        }

        let postings_per_term: Vec<&[Posting]> = terms
            .iter()
            .map(|t| self.get_postings(t))
            .collect();

        if postings_per_term.iter().any(|p| p.is_empty()) {
            return vec![];
        }

        let mut candidates = Self::postings_to_doc_ids(postings_per_term[0]);
        for postings in &postings_per_term[1..] {
            candidates = intersect(&candidates, &Self::postings_to_doc_ids(postings));
        }

        candidates
            .into_iter()
            .filter(|&doc_id| self.verify_phrase(doc_id, &postings_per_term))
            .collect()
    }

    fn verify_phrase(&self, doc_id: DocID, postings_per_term: &[&[Posting]]) -> bool {
        let position_sets: Vec<&[u32]> = postings_per_term
            .iter()
            .map(|postings| {
                postings
                    .iter()
                    .find(|p| p.doc_id == doc_id)
                    .map(|p| p.positions.as_slice())
                    .unwrap_or(&[])
            })
            .collect();

        if position_sets.iter().any(|p| p.is_empty()) {
            return false;
        }

        position_sets[0].iter().any(|&start| {
            (1..position_sets.len()).all(|i| {
                let target = start + i as u32;
                position_sets[i].binary_search(&target).is_ok()
            })
        })
    }
}

fn intersect(a: &[DocID], b: &[DocID]) -> Vec<DocID> {
    let mut result = Vec::new();
    let (mut i, mut j) = (0, 0);
    while i < a.len() && j < b.len() {
        match a[i].cmp(&b[j]) {
            std::cmp::Ordering::Equal => {
                result.push(a[i]);
                i += 1;
                j += 1;
            }
            std::cmp::Ordering::Less => i += 1,
            std::cmp::Ordering::Greater => j += 1,
        }
    }
    result
}

fn union(a: &[DocID], b: &[DocID]) -> Vec<DocID> {
    let mut result = Vec::new();
    let (mut i, mut j) = (0, 0);
    while i < a.len() && j < b.len() {
        match a[i].cmp(&b[j]) {
            std::cmp::Ordering::Equal => {
                result.push(a[i]);
                i += 1;
                j += 1;
            }
            std::cmp::Ordering::Less => {
                result.push(a[i]);
                i += 1;
            }
            std::cmp::Ordering::Greater => {
                result.push(b[j]);
                j += 1;
            }
        }
    }
    result.extend_from_slice(&a[i..]);
    result.extend_from_slice(&b[j..]);
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    fn setup_index() -> InvertedIndex {
        let mut idx = InvertedIndex::new(&[]);
        idx.add_document(1, "The quick brown fox jumps over the lazy dog");
        idx.add_document(2, "A fast brown cat sits on the lazy mat");
        idx.add_document(3, "Quick systems design for distributed databases");
        idx.add_document(4, "The dog chased the cat across the yard");
        idx
    }

    #[test]
    fn test_and_query() {
        let idx = setup_index();
        assert_eq!(idx.search_and(&["brown", "lazy"]), vec![1, 2]);
    }

    #[test]
    fn test_or_query() {
        let idx = setup_index();
        assert_eq!(idx.search_or(&["dog", "cat"]), vec![1, 2, 4]);
    }

    #[test]
    fn test_not_query() {
        let idx = setup_index();
        let base = idx.search_or(&["brown", "dog"]);
        assert_eq!(idx.search_not(&base, "cat"), vec![1]);
    }

    #[test]
    fn test_phrase_query() {
        let idx = setup_index();
        assert_eq!(idx.search_phrase(&["brown", "fox"]), vec![1]);
    }

    #[test]
    fn test_phrase_wrong_order() {
        let idx = setup_index();
        assert!(idx.search_phrase(&["fox", "brown"]).is_empty());
    }

    #[test]
    fn test_empty_result() {
        let idx = setup_index();
        assert!(idx.search_and(&["nonexistent"]).is_empty());
    }

    #[test]
    fn test_stop_words_filtered() {
        let idx = setup_index();
        assert!(idx.get_postings("the").is_empty());
    }

    #[test]
    fn test_unicode() {
        let mut idx = InvertedIndex::new(&[]);
        idx.add_document(1, "El cafe esta caliente");
        idx.add_document(2, "Busqueda rapida y eficiente");
        assert_eq!(idx.search_and(&["cafe"]), vec![1]);
    }

    #[test]
    fn test_repeated_terms() {
        let mut idx = InvertedIndex::new(&[]);
        idx.add_document(1, "search search search engine");
        let postings = idx.get_postings("search");
        assert_eq!(postings.len(), 1);
        assert_eq!(postings[0].frequency, 3);
    }
}
```

### Run and verify

```bash
cd inverted-index
cargo test
```

Expected output:

```
running 9 tests
test tests::test_and_query ... ok
test tests::test_or_query ... ok
test tests::test_not_query ... ok
test tests::test_phrase_query ... ok
test tests::test_phrase_wrong_order ... ok
test tests::test_empty_result ... ok
test tests::test_stop_words_filtered ... ok
test tests::test_unicode ... ok
test tests::test_repeated_terms ... ok

test result: ok. 9 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **BTreeMap for index (Rust) vs HashMap (Go)**: BTreeMap gives sorted term iteration, useful for debugging and prefix queries. Go's map lacks ordering, which is acceptable since we don't need sorted term enumeration for Boolean queries.

2. **Positions stored as raw word offsets including stop words**: Positions reference the original token sequence. This means stop-word removal does not shift positions, preserving correct phrase adjacency checks. If position 5 is "brown" and position 6 is "fox", removing "the" at position 4 doesn't change the gap between them.

3. **Posting lists sorted by document ID**: Enables O(n+m) merge-based intersection and union. This is the standard approach in information retrieval and avoids the O(n*m) cost of nested loops.

4. **Binary search for position verification**: During phrase queries, position lists are sorted, so binary search confirms adjacency in O(log p) per position check instead of linear scan.

## Common Mistakes

1. **Shifting positions after stop-word removal**: If you re-number positions after filtering stop words, phrase queries break because the positions no longer reflect the original word order. Keep original positions.

2. **Case-sensitive indexing**: Forgetting to normalize case means "Quick" and "quick" produce separate posting lists that never intersect.

3. **Quadratic intersection**: Using `contains()` on one list for every element of another gives O(n*m). Use the two-pointer merge on sorted lists for O(n+m).

4. **Empty posting list handling**: Forgetting to short-circuit when any term has zero postings causes index-out-of-bounds or incorrect results in AND/phrase queries.

## Performance Notes

- **Index build**: O(D * T) where D is document count and T is average tokens per document. Each token requires a hash lookup and position append.
- **AND query**: O(P1 + P2 + ... + Pk) where Pi is the posting list length for term i. Optimization: process shortest posting list first.
- **Phrase query**: O(AND cost + sum of position list lengths for candidate documents). Binary search on positions reduces the constant factor.
- **Memory**: Each posting stores a doc ID (4 bytes), frequency (4 bytes), and position slice. For a corpus of 1M documents with average 500 unique terms per document, expect ~2-4 GB of index memory. Production systems use variable-byte encoding to compress posting lists.
