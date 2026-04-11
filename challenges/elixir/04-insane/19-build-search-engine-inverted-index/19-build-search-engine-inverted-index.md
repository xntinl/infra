# Full-Text Search Engine with BM25 Ranking

**Project**: `searcher` — a full-text search engine with inverted index, BM25 ranking, and phrase queries

---

## Project context

You are building `searcher`, a full-text search engine from scratch — no Elasticsearch, no Solr, no external search libraries. The engine processes text through an NLP pipeline, builds an inverted index with positional information, and ranks results using BM25.

Project structure:

```
searcher/
├── lib/
│   └── searcher/
│       ├── application.ex           # engine supervisor
│       ├── engine.ex                # public API: index, search, phrase_search, delete
│       ├── pipeline.ex              # NLP pipeline: tokenize, lowercase, stop-words, stem
│       ├── stemmer.ex               # Porter stemmer: 5-phase suffix reduction algorithm
│       ├── inverted_index.ex        # ETS-backed index: term → [{doc_id, tf, [positions]}]
│       ├── scorer.ex                # TF-IDF and BM25 ranking functions
│       ├── boolean_query.ex         # AND, OR, NOT via posting list operations
│       ├── phrase_query.ex          # positional intersection for phrase matching
│       └── tombstone.ex             # deleted doc tracking, exclusion from results
├── test/
│   └── searcher/
│       ├── pipeline_test.exs        # tokenization, stop-words, stemming correctness
│       ├── stemmer_test.exs         # Porter algorithm phase-by-phase
│       ├── index_test.exs           # posting list structure and updates
│       ├── bm25_test.exs            # scoring formula, length normalization
│       ├── boolean_test.exs         # AND/OR/NOT semantics
│       └── phrase_test.exs          # positional matching
├── bench/
│   └── searcher_bench.exs
└── mix.exs
```

---

## The problem

Given a corpus of 1 million documents, you need to answer queries like "machine learning" and return the 10 most relevant documents in under 50ms. A full table scan that compares every document against the query is O(N × Q), which is too slow. An inverted index reduces this to O(|postings list|) — you fetch only the documents that contain the query terms.

Ranking is the second problem. A document that mentions "machine" 100 times is not necessarily more relevant than one that mentions it 5 times in a 50-word abstract. BM25 normalizes for document length and uses a diminishing-returns model for term frequency.

---

## Why this design

**Inverted index for sub-linear query time**: instead of "for each document, does it contain term X?", the index stores "for term X, here are all documents that contain it." Querying is O(|posting list for X|) instead of O(N). For a corpus of 1M documents with a 10,000-document average posting list, this is a 100x speedup.

**Porter stemmer for morphological normalization**: "running", "runs", and "ran" all stem to the same token. Without stemming, a query for "run" misses documents that say "running." The Porter algorithm applies five phases of suffix-stripping rules deterministically. It is not linguistically perfect but is fast and works well for English information retrieval.

**BM25 over TF-IDF for length normalization**: TF-IDF gives higher scores to longer documents simply because they have more term occurrences. BM25 saturates the term frequency contribution (controlled by k1) and penalizes documents longer than the corpus average (controlled by b). The parameters k1=1.5, b=0.75 are empirically validated defaults.

**Positional index for phrase queries**: a basic inverted index stores (doc_id, term_frequency). A positional index also stores the word offset of each occurrence. To answer "machine learning", you find documents where "machine" occurs at position P and "learning" at position P+1. Without positions, you cannot distinguish "machine learning" from two unrelated occurrences.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new searcher --sup
cd searcher
mkdir -p lib/searcher test/searcher bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: NLP pipeline

```elixir
# lib/searcher/pipeline.ex
defmodule Searcher.Pipeline do
  @stop_words MapSet.new(~w[
    a an the and or not is are was were be been being have has had
    do does did will would shall should may might must can could
    i me my myself we our you your he she it its they them their
    this that these those what which who whom when where why how
    in on at by for with about against between into through during
    before after above below to from up down out of off over under
  ])

  @doc "Runs the full pipeline on text. Returns [{stemmed_token, original_position}]."
  def process(text) do
    text
    |> tokenize()
    |> Enum.with_index()
    |> Enum.reject(fn {token, _pos} -> stop_word?(token) end)
    |> Enum.map(fn {token, pos} -> {Searcher.Stemmer.stem(token), pos} end)
  end

  defp tokenize(text) do
    text
    |> String.downcase()
    |> String.split(~r/[^a-z0-9']+/, trim: true)
  end

  defp stop_word?(token), do: MapSet.member?(@stop_words, token)
end
```

### Step 4: BM25 scorer

```elixir
# lib/searcher/scorer.ex
defmodule Searcher.Scorer do
  @moduledoc """
  BM25 scoring.

  score(d, q) = Σ_t [ IDF(t) × (tf(t,d) × (k1 + 1)) / (tf(t,d) + k1 × (1 - b + b × len(d)/avg_len)) ]

  IDF(t) = log((N - df(t) + 0.5) / (df(t) + 0.5) + 1)

  where:
    N     = total number of documents
    df(t) = number of documents containing term t
    tf    = term frequency in document d
    len(d) = length of document d (in tokens)
    avg_len = average document length across corpus
  """

  @k1 1.5
  @b  0.75

  def bm25(tf, df, doc_len, avg_len, total_docs) do
    # TODO: implement the formula above
  end

  def idf(df, total_docs) do
    # TODO: :math.log((total_docs - df + 0.5) / (df + 0.5) + 1)
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/searcher/pipeline_test.exs
defmodule Searcher.PipelineTest do
  use ExUnit.Case, async: true

  test "tokenizes and removes stop words" do
    tokens = Searcher.Pipeline.process("Hello, world! It's a test.")
    stems = Enum.map(tokens, fn {stem, _pos} -> stem end)
    assert "hello" in stems
    assert "world" in stems
    assert "test" in stems
    refute "a" in stems
    refute "it" in stems
  end

  test "preserves positions after stop-word removal" do
    tokens = Searcher.Pipeline.process("the quick brown fox")
    # "the" is a stop word; positions should reflect original offsets
    positions = Enum.map(tokens, fn {_, pos} -> pos end)
    assert positions == Enum.sort(positions), "positions must be ascending"
  end
end
```

```elixir
# test/searcher/stemmer_test.exs
defmodule Searcher.StemmerTest do
  use ExUnit.Case, async: true

  test "running/runs/run all stem to the same token" do
    stems = Enum.map(~w[running runs runner], &Searcher.Stemmer.stem/1)
    assert length(Enum.uniq(stems)) == 1, "expected same stem, got: #{inspect(Enum.uniq(stems))}"
  end

  test "caresses -> caress" do
    assert Searcher.Stemmer.stem("caresses") == "caress"
  end

  test "agreed -> agre" do
    # Porter phase 1a: "eed" → "ee"... verify against Porter reference
    # The exact stem depends on your phase implementation; match the reference
    result = Searcher.Stemmer.stem("agreed")
    assert is_binary(result) and byte_size(result) > 0
  end
end
```

```elixir
# test/searcher/bm25_test.exs
defmodule Searcher.BM25Test do
  use ExUnit.Case, async: false

  setup do
    {:ok, engine} = Searcher.Engine.start_link()
    {:ok, engine: engine}
  end

  test "short document with term scores higher than long document with same TF", %{engine: engine} do
    # Short doc: 10 words, "machine" appears once
    Searcher.Engine.index(engine, "short", "machine learning is great " <> Enum.join(for(_ <- 1..6, do: "padding"), " "))

    # Long doc: 500 words, "machine" appears once
    Searcher.Engine.index(engine, "long", "machine " <> Enum.join(for(_ <- 1..499, do: "padding"), " "))

    results = Searcher.Engine.search(engine, "machine", scorer: :bm25, top_k: 2)
    ids = Enum.map(results, fn {id, _score} -> id end)

    assert List.first(ids) == "short",
      "short document must rank higher: #{inspect(results)}"
  end
end
```

### Step 6: Run the tests

```bash
mix test test/searcher/ --trace
```

### Step 7: Benchmark

```elixir
# bench/searcher_bench.exs
{:ok, engine} = Searcher.Engine.start_link()

# Index 100,000 documents
for i <- 1..100_000 do
  Searcher.Engine.index(engine, "doc_#{i}",
    "#{Enum.random(~w[machine learning deep neural network training model optimization])} " <>
    Enum.join(for(_ <- 1..:rand.uniform(200), do: "word"), " "))
end

Benchee.run(
  %{
    "search — single term BM25 top-10" => fn ->
      Searcher.Engine.search(engine, "machine", scorer: :bm25, top_k: 10)
    end,
    "search — AND query" => fn ->
      Searcher.Engine.search(engine, "machine AND learning", scorer: :bm25, top_k: 10)
    end,
    "phrase search" => fn ->
      Searcher.Engine.phrase_search(engine, "machine learning")
    end
  },
  parallel: 4,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Target: top-10 BM25 query under 50ms on a 1M-document corpus.

---

## Trade-off analysis

| Aspect | TF-IDF | BM25 | BM25F (field-weighted) |
|--------|--------|------|----------------------|
| Length normalization | none | controlled by `b` | per-field `b` |
| TF saturation | linear | saturating (`k1`) | per-field `k1` |
| Implementation | trivial | moderate | complex |
| Performance on short docs | poor (no length penalty) | good | best |
| Parameters to tune | none | k1, b | k1f, bf per field |

Reflection: BM25 treats all document fields equally. A document where "machine learning" appears in the title ranks the same as one where it appears once in a 10,000-word body. What change to the scoring formula would weight title matches higher than body matches?

---

## Common production mistakes

**1. Porter stemmer implemented without all 5 phases**
The Porter algorithm has 5 phases. Implementing only phases 1-2 and calling it done produces a stemmer that mis-stems many common English words. Implement all five phases and test against the Porter reference word list.

**2. Stop-word list too aggressive**
Removing "not" from queries means "machine learning is NOT classification" becomes "machine learning classification" — the negation is lost. Stop-word lists should be conservative; only remove words that carry zero information value (articles, prepositions).

**3. BM25 average document length computed at index time only**
If documents are added incrementally, the average length changes. IDF also changes as `total_docs` grows. Recompute `avg_len` and `total_docs` from ETS metadata on every search, not from a cached value set at startup.

**4. Phrase query using global visited set**
Positional intersection for phrases must check consecutive positions per document. Using a global set instead of per-document checking produces incorrect results when the same term appears at different positions in different documents.

---

## Resources

- Manning, C., Raghavan, P. & Schütze, H. — *Introduction to Information Retrieval* — [free online](https://nlp.stanford.edu/IR-book/)
- Porter, M.F. (1980). *An algorithm for suffix stripping* — the original stemmer paper
- Robertson, S. & Zaragoza, H. (2009). *The Probabilistic Relevance Framework: BM25 and Beyond*
- [Okapi BM25 on Wikipedia](https://en.wikipedia.org/wiki/Okapi_BM25) — formula and parameter guidance
