# 52. Inverted Index Text Search

<!--
difficulty: intermediate-advanced
category: search-engines-text-processing
languages: [go, rust]
concepts: [inverted-index, tokenization, boolean-queries, phrase-search, stop-words, positional-index]
estimated_time: 6-8 hours
bloom_level: analyze
prerequisites: [hash-maps, string-processing, file-io, iterators, error-handling]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Hash maps and their performance characteristics
- String splitting, Unicode-aware text processing
- File I/O and reading structured text
- Iterator patterns (Rust) / slice operations (Go)
- Basic understanding of information retrieval concepts

## Learning Objectives

- **Implement** an inverted index that maps terms to document IDs with positional metadata
- **Apply** text normalization techniques: tokenization, case folding, stop-word removal
- **Analyze** how positional information enables phrase query resolution beyond simple term matching
- **Design** a query evaluator supporting Boolean operators (AND/OR/NOT) over posting lists
- **Evaluate** trade-offs between index size and query capability when storing positions vs frequencies only

## The Challenge

Search engines answer queries in milliseconds across billions of documents. The core data structure enabling this is the inverted index: instead of storing "document -> words", you store "word -> documents". When a user searches for "distributed systems", you look up both terms and intersect their posting lists, avoiding a scan of every document.

But simple term lookup is not enough. Users expect phrase search ("distributed systems" as an exact phrase), Boolean logic (distributed AND NOT monolithic), and fast results even as the corpus grows. Phrase search requires positional information: you must know not just that "distributed" appears in document 7, but that it appears at position 3, and "systems" appears at position 4.

Build an inverted index from scratch that ingests plain-text documents, tokenizes and normalizes their content, and answers Boolean and phrase queries. The index must store term positions so phrase queries can verify adjacency. Both Go and Rust implementations are required.

## Requirements

1. Implement a **tokenizer** that splits text on whitespace and punctuation, producing lowercase tokens. Handle Unicode correctly (accented characters, multi-byte sequences)
2. Implement **stop-word removal** with a configurable set of stop words (at least: "the", "a", "an", "is", "are", "was", "in", "of", "to", "and", "or", "for", "on", "it", "at")
3. Build an **inverted index** where each term maps to a posting list. Each posting entry contains: document ID, term frequency in that document, and a list of positions (0-based word offsets)
4. Implement **AND queries**: return documents containing all query terms (intersection of posting lists)
5. Implement **OR queries**: return documents containing any query term (union of posting lists)
6. Implement **NOT queries**: exclude documents containing a term from a result set
7. Implement **phrase queries**: given an ordered sequence of terms, return documents where those terms appear consecutively. Use positional data to verify adjacency
8. Support **compound queries** combining operators: `(distributed AND systems) NOT monolithic`
9. Provide an `add_document(id, text)` method and a `search(query) -> Vec<DocId>` method
10. Write tests covering: single-term, multi-term AND/OR/NOT, phrase queries, empty results, documents with repeated terms, Unicode content

## Hints

<details>
<summary>Hint 1: Posting list structure</summary>

A posting list is a sorted sequence of entries. Keeping it sorted by document ID enables efficient intersection via a merge-join (two pointers advancing through sorted lists):

```
Term "search" -> [(doc:1, freq:3, pos:[0, 15, 42]), (doc:4, freq:1, pos:[7])]
```

In Rust, use `BTreeMap<String, Vec<Posting>>`. In Go, `map[string][]Posting` with explicit sort after building.
</details>

<details>
<summary>Hint 2: Efficient posting list intersection</summary>

AND queries intersect posting lists. The optimal algorithm uses two pointers on sorted lists:

```
fn intersect(a: &[Posting], b: &[Posting]) -> Vec<DocId> {
    let (mut i, mut j) = (0, 0);
    let mut result = vec![];
    while i < a.len() && j < b.len() {
        match a[i].doc_id.cmp(&b[j].doc_id) {
            Equal => { result.push(a[i].doc_id); i += 1; j += 1; }
            Less => i += 1,
            Greater => j += 1,
        }
    }
    result
}
```

This runs in O(n + m) where n and m are the posting list lengths.
</details>

<details>
<summary>Hint 3: Phrase queries via positional check</summary>

After finding documents that contain all terms in the phrase, verify adjacency. For a phrase of terms [t0, t1, t2], find positions where `pos(t1) = pos(t0) + 1` and `pos(t2) = pos(t0) + 2`. This is another merge-join, but on position lists offset by the term's index in the phrase.
</details>

<details>
<summary>Hint 4: Simple query parser approach</summary>

For compound queries, you do not need a full parser. A simple approach: split the query on Boolean operators, evaluate each sub-expression against the index, then combine results with set operations (intersection, union, difference). Handle parentheses with a small recursive descent parser or shunting-yard algorithm.
</details>

## Acceptance Criteria

- [ ] Tokenizer correctly splits text on whitespace/punctuation and lowercases all tokens
- [ ] Stop words are removed during indexing (configurable list)
- [ ] Inverted index stores correct term frequencies and positions for each document
- [ ] AND queries return only documents containing all terms
- [ ] OR queries return documents containing any term
- [ ] NOT queries exclude documents from a result set
- [ ] Phrase queries correctly identify consecutive term occurrences using positions
- [ ] Compound queries with mixed operators produce correct results
- [ ] Unicode text is handled correctly (accented characters, multi-byte)
- [ ] All tests pass in both Go (`go test ./...`) and Rust (`cargo test`)

## Research Resources

- [Introduction to Information Retrieval, Ch. 1-2](https://nlp.stanford.edu/IR-book/information-retrieval-book.html) -- Manning, Raghavan, Schutze; free online textbook covering inverted index construction and Boolean retrieval
- [Inverted Index (Wikipedia)](https://en.wikipedia.org/wiki/Inverted_index) -- overview of the data structure and its variants
- [Lucene in Action, Ch. 1](https://www.manning.com/books/lucene-in-action) -- practical inverted index construction in a real search engine
- [Go strings package](https://pkg.go.dev/strings) -- string manipulation functions for tokenization
- [Rust unicode-segmentation crate](https://docs.rs/unicode-segmentation) -- Unicode-aware word boundary detection
- [The Anatomy of a Large-Scale Search Engine (Brin & Page)](http://infolab.stanford.edu/~backrub/google.html) -- original Google paper describing inverted index use at scale
