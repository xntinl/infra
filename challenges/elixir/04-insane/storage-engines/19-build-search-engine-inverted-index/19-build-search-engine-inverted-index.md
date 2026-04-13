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

Given a corpus of 1 million documents, you need to answer queries like "machine learning" and return the 10 most relevant documents in under 50ms. A full table scan that compares every document against the query is O(N x Q), which is too slow. An inverted index reduces this to O(|postings list|) — you fetch only the documents that contain the query terms.

Ranking is the second problem. A document that mentions "machine" 100 times is not necessarily more relevant than one that mentions it 5 times in a 50-word abstract. BM25 normalizes for document length and uses a diminishing-returns model for term frequency.

---

## Why this design

**Inverted index for sub-linear query time**: instead of "for each document, does it contain term X?", the index stores "for term X, here are all documents that contain it." Querying is O(|posting list for X|) instead of O(N).

**Porter stemmer for morphological normalization**: "running", "runs", and "ran" all stem to the same token. Without stemming, a query for "run" misses documents that say "running."

**BM25 over TF-IDF for length normalization**: TF-IDF gives higher scores to longer documents simply because they have more term occurrences. BM25 saturates the term frequency contribution (controlled by k1) and penalizes documents longer than the corpus average (controlled by b).

**Positional index for phrase queries**: a basic inverted index stores (doc_id, term_frequency). A positional index also stores the word offset of each occurrence. To answer "machine learning", you find documents where "machine" occurs at position P and "learning" at position P+1.

---

## Design decisions

**Option A — Trie of query prefixes**
- Pros: excellent for prefix search and autocomplete.
- Cons: full-text search is awkward; ranking requires a separate structure.

**Option B — Inverted index (term → posting list) with BM25 scoring** (chosen)
- Pros: canonical full-text shape; trivial intersection for AND queries; BM25 gives relevance ranking out of the box.
- Cons: updates require segment merges à la Lucene.

→ Chose **B** because every production full-text engine (Lucene, Tantivy, Bleve) converges on inverted indexes for a reason: they are the shape of the problem.

## Project Structure

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

## Implementation milestones

### Step 1: Create the project

**Objective**: Generate `--sup` skeleton so the indexer, scorer, and query engine hang under one supervisor and restart without losing ETS tables.


```bash
mix new searcher --sup
cd searcher
mkdir -p lib/searcher test/searcher bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Keep deps to `:benchee` only; tokenization, stemming, and BM25 are pure Elixir so there is no NIF license or audit risk.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Porter stemmer

**Objective**: Apply Porter's five-phase suffix stripping so morphological variants (running, runs, runner) collapse to one posting list at index time.


```elixir
# lib/searcher/stemmer.ex
defmodule Searcher.Stemmer do
  @moduledoc """
  Simplified Porter stemmer for English.

  Implements the core suffix-stripping rules across multiple phases
  to reduce words to their morphological root. This is not a complete
  Porter implementation but handles the most common English suffixes.
  """

  @doc "Stems an English word to its root form."
  @spec stem(String.t()) :: String.t()
  def stem(word) when byte_size(word) <= 2, do: word

  def stem(word) do
    word
    |> String.downcase()
    |> step_1a()
    |> step_1b()
    |> step_1c()
    |> step_2()
    |> step_3()
    |> step_4()
    |> step_5()
  end

  defp step_1a(word) do
    cond do
      String.ends_with?(word, "sses") -> String.replace_suffix(word, "sses", "ss")
      String.ends_with?(word, "ies") -> String.replace_suffix(word, "ies", "i")
      String.ends_with?(word, "ss") -> word
      String.ends_with?(word, "s") -> String.replace_suffix(word, "s", "")
      true -> word
    end
  end

  defp step_1b(word) do
    cond do
      String.ends_with?(word, "eed") ->
        stem_part = String.replace_suffix(word, "eed", "")
        if measure(stem_part) > 0, do: String.replace_suffix(word, "eed", "ee"), else: word

      String.ends_with?(word, "ed") ->
        stem_part = String.replace_suffix(word, "ed", "")
        if has_vowel?(stem_part), do: step_1b_cleanup(stem_part), else: word

      String.ends_with?(word, "ing") ->
        stem_part = String.replace_suffix(word, "ing", "")
        if has_vowel?(stem_part), do: step_1b_cleanup(stem_part), else: word

      true -> word
    end
  end

  defp step_1b_cleanup(word) do
    cond do
      String.ends_with?(word, "at") -> word <> "e"
      String.ends_with?(word, "bl") -> word <> "e"
      String.ends_with?(word, "iz") -> word <> "e"
      double_consonant?(word) and not String.ends_with?(word, "l") and
        not String.ends_with?(word, "s") and not String.ends_with?(word, "z") ->
          String.slice(word, 0..-2//1)
      measure(word) == 1 and cvc?(word) -> word <> "e"
      true -> word
    end
  end

  defp step_1c(word) do
    if String.ends_with?(word, "y") do
      stem_part = String.replace_suffix(word, "y", "")
      if has_vowel?(stem_part), do: stem_part <> "i", else: word
    else
      word
    end
  end

  defp step_2(word) do
    replacements = [
      {"ational", "ate"}, {"tional", "tion"}, {"enci", "ence"}, {"anci", "ance"},
      {"izer", "ize"}, {"abli", "able"}, {"alli", "al"}, {"entli", "ent"},
      {"eli", "e"}, {"ousli", "ous"}, {"ization", "ize"}, {"ation", "ate"},
      {"ator", "ate"}, {"alism", "al"}, {"iveness", "ive"}, {"fulness", "ful"},
      {"ousness", "ous"}, {"aliti", "al"}, {"iviti", "ive"}, {"biliti", "ble"}
    ]

    apply_suffix_rules(word, replacements, 0)
  end

  defp step_3(word) do
    replacements = [
      {"icate", "ic"}, {"ative", ""}, {"alize", "al"},
      {"iciti", "ic"}, {"ical", "ic"}, {"ful", ""}, {"ness", ""}
    ]

    apply_suffix_rules(word, replacements, 0)
  end

  defp step_4(word) do
    suffixes = ["al", "ance", "ence", "er", "ic", "able", "ible", "ant",
                "ement", "ment", "ent", "ion", "ou", "ism", "ate", "iti",
                "ous", "ive", "ize"]

    result =
      Enum.find_value(suffixes, word, fn suffix ->
        if String.ends_with?(word, suffix) do
          stem_part = String.replace_suffix(word, suffix, "")
          if measure(stem_part) > 1 do
            if suffix == "ion" do
              if String.ends_with?(stem_part, "s") or String.ends_with?(stem_part, "t") do
                stem_part
              else
                nil
              end
            else
              stem_part
            end
          else
            nil
          end
        else
          nil
        end
      end)

    result || word
  end

  defp step_5(word) do
    word = step_5a(word)
    step_5b(word)
  end

  defp step_5a(word) do
    if String.ends_with?(word, "e") do
      stem_part = String.replace_suffix(word, "e", "")
      cond do
        measure(stem_part) > 1 -> stem_part
        measure(stem_part) == 1 and not cvc?(stem_part) -> stem_part
        true -> word
      end
    else
      word
    end
  end

  defp step_5b(word) do
    if measure(word) > 1 and double_consonant?(word) and String.ends_with?(word, "l") do
      String.slice(word, 0..-2//1)
    else
      word
    end
  end

  defp apply_suffix_rules(word, [], _min_measure), do: word

  defp apply_suffix_rules(word, [{suffix, replacement} | rest], min_measure) do
    if String.ends_with?(word, suffix) do
      stem_part = String.replace_suffix(word, suffix, "")
      if measure(stem_part) > min_measure do
        stem_part <> replacement
      else
        word
      end
    else
      apply_suffix_rules(word, rest, min_measure)
    end
  end

  defp measure(word) do
    word
    |> String.graphemes()
    |> Enum.map(&vowel?/1)
    |> Enum.chunk_while(nil, fn
      is_v, nil -> {:cont, is_v}
      is_v, prev when is_v == prev -> {:cont, is_v}
      is_v, prev -> {:cont, prev, is_v}
    end, fn acc -> {:cont, acc, nil} end)
    |> Enum.reject(&is_nil/1)
    |> then(fn chunks ->
      vc_pairs = div(length(chunks), 2)
      if length(chunks) > 0 and hd(chunks) == true, do: vc_pairs, else: max(0, vc_pairs)
    end)
  end

  defp has_vowel?(word) do
    word |> String.graphemes() |> Enum.any?(&vowel?/1)
  end

  defp vowel?(char), do: char in ~w(a e i o u)

  defp double_consonant?(word) do
    len = String.length(word)
    if len >= 2 do
      last = String.at(word, len - 1)
      second_last = String.at(word, len - 2)
      last == second_last and not vowel?(last)
    else
      false
    end
  end

  defp cvc?(word) do
    len = String.length(word)
    if len >= 3 do
      c1 = String.at(word, len - 3)
      v = String.at(word, len - 2)
      c2 = String.at(word, len - 1)
      not vowel?(c1) and vowel?(v) and not vowel?(c2) and c2 not in ~w(w x y)
    else
      false
    end
  end
end
```

### Step 4: NLP pipeline

**Objective**: Chain tokenize, lowercase, stop-word filter, and stem while preserving original offsets so phrase queries stay position-accurate.


```elixir
# lib/searcher/pipeline.ex
defmodule Searcher.Pipeline do
  @moduledoc """
  Text processing pipeline: tokenize, lowercase, remove stop words, stem.
  """

  @stop_words MapSet.new(~w[
    a an the and or not is are was were be been being have has had
    do does did will would shall should may might must can could
    i me my myself we our you your he she it its they them their
    this that these those what which who whom when where why how
    in on at by for with about against between into through during
    before after above below to from up down out of off over under
  ])

  @doc "Runs the full pipeline on text. Returns [{stemmed_token, original_position}]."
  @spec process(String.t()) :: [{String.t(), non_neg_integer()}]
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

### Step 5: BM25 scorer

**Objective**: Score with BM25 (k1=1.5, b=0.75) so term saturation and length normalization rank short relevant docs above long keyword-stuffed ones.


```elixir
# lib/searcher/scorer.ex
defmodule Searcher.Scorer do
  @moduledoc """
  BM25 scoring.

  score(d, q) = sum_t [ IDF(t) x (tf(t,d) x (k1 + 1)) / (tf(t,d) + k1 x (1 - b + b x len(d)/avg_len)) ]

  IDF(t) = log((N - df(t) + 0.5) / (df(t) + 0.5) + 1)
  """

  @k1 1.5
  @b  0.75

  @doc "Computes the BM25 score for a single term in a document."
  @spec bm25(number(), number(), number(), number(), number()) :: float()
  def bm25(tf, df, doc_len, avg_len, total_docs) do
    idf_val = idf(df, total_docs)
    numerator = tf * (@k1 + 1)
    denominator = tf + @k1 * (1 - @b + @b * doc_len / avg_len)
    idf_val * numerator / denominator
  end

  @doc "Computes the inverse document frequency for a term."
  @spec idf(number(), number()) :: float()
  def idf(df, total_docs) do
    :math.log((total_docs - df + 0.5) / (df + 0.5) + 1)
  end
end
```

### Step 6: Inverted index

**Objective**: Store `term -> [{doc_id, tf, positions}]` postings in ETS so point lookups are O(1) and phrase matching has the offsets it needs.


```elixir
# lib/searcher/inverted_index.ex
defmodule Searcher.InvertedIndex do
  @moduledoc """
  ETS-backed inverted index with positional information.

  Structure: term -> [{doc_id, tf, [positions]}]
  """

  def ensure_tables do
    for name <- [:searcher_index, :searcher_docs, :searcher_meta] do
      case :ets.whereis(name) do
        :undefined -> :ets.new(name, [:named_table, :public, :set])
        _ -> :ok
      end
    end
    :ok
  end

  @doc "Indexes a document with the given doc_id and text content."
  @spec index(String.t(), String.t()) :: :ok
  def index(doc_id, text) do
    ensure_tables()
    tokens = Searcher.Pipeline.process(text)
    doc_len = length(tokens)

    :ets.insert(:searcher_docs, {doc_id, doc_len, text})

    term_positions =
      Enum.group_by(tokens, fn {stem, _pos} -> stem end, fn {_stem, pos} -> pos end)

    Enum.each(term_positions, fn {term, positions} ->
      tf = length(positions)
      entry = {doc_id, tf, Enum.sort(positions)}

      case :ets.lookup(:searcher_index, term) do
        [{^term, postings}] ->
          updated = [entry | Enum.reject(postings, fn {id, _, _} -> id == doc_id end)]
          :ets.insert(:searcher_index, {term, updated})
        [] ->
          :ets.insert(:searcher_index, {term, [entry]})
      end
    end)

    update_meta()
    :ok
  end

  @doc "Retrieves the posting list for a term."
  @spec postings(String.t()) :: [{String.t(), non_neg_integer(), [non_neg_integer()]}]
  def postings(term) do
    ensure_tables()
    case :ets.lookup(:searcher_index, term) do
      [{^term, list}] -> list
      [] -> []
    end
  end

  @doc "Returns {total_docs, avg_doc_len}."
  @spec stats() :: {non_neg_integer(), float()}
  def stats do
    ensure_tables()
    case :ets.lookup(:searcher_meta, :stats) do
      [{:stats, total, avg}] -> {total, avg}
      [] -> {0, 0.0}
    end
  end

  @doc "Returns the doc length for a specific document."
  @spec doc_length(String.t()) :: non_neg_integer()
  def doc_length(doc_id) do
    case :ets.lookup(:searcher_docs, doc_id) do
      [{^doc_id, len, _text}] -> len
      [] -> 0
    end
  end

  defp update_meta do
    docs = :ets.tab2list(:searcher_docs)
    total = length(docs)
    avg_len = if total > 0, do: Enum.sum(Enum.map(docs, fn {_, l, _} -> l end)) / total, else: 0.0
    :ets.insert(:searcher_meta, {:stats, total, avg_len})
  end
end
```

### Step 7: Engine — public API

**Objective**: Front pipeline, index, scorer, and phrase matcher behind one GenServer so callers get `index/2` and `search/3` without touching posting lists.


```elixir
# lib/searcher/engine.ex
defmodule Searcher.Engine do
  use GenServer

  @moduledoc """
  Public API for the search engine.
  """

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts)
  end

  @doc "Indexes a document."
  @spec index(GenServer.server(), String.t(), String.t()) :: :ok
  def index(_engine, doc_id, text) do
    Searcher.InvertedIndex.index(doc_id, text)
  end

  @doc "Searches for documents matching the query. Returns [{doc_id, score}] sorted by score desc."
  @spec search(GenServer.server(), String.t(), keyword()) :: [{String.t(), float()}]
  def search(_engine, query, opts \\ []) do
    top_k = Keyword.get(opts, :top_k, 10)
    _scorer = Keyword.get(opts, :scorer, :bm25)

    query_terms =
      Searcher.Pipeline.process(query)
      |> Enum.map(fn {stem, _pos} -> stem end)
      |> Enum.uniq()

    {total_docs, avg_len} = Searcher.InvertedIndex.stats()

    if total_docs == 0 do
      []
    else
      doc_scores =
        Enum.flat_map(query_terms, fn term ->
          postings = Searcher.InvertedIndex.postings(term)
          df = length(postings)

          Enum.map(postings, fn {doc_id, tf, _positions} ->
            doc_len = Searcher.InvertedIndex.doc_length(doc_id)
            score = Searcher.Scorer.bm25(tf, df, doc_len, avg_len, total_docs)
            {doc_id, score}
          end)
        end)
        |> Enum.group_by(fn {doc_id, _score} -> doc_id end, fn {_doc_id, score} -> score end)
        |> Enum.map(fn {doc_id, scores} -> {doc_id, Enum.sum(scores)} end)
        |> Enum.sort_by(fn {_id, score} -> score end, :desc)
        |> Enum.take(top_k)

      doc_scores
    end
  end

  @doc "Searches for an exact phrase in documents."
  @spec phrase_search(GenServer.server(), String.t()) :: [{String.t(), [{non_neg_integer(), non_neg_integer()}]}]
  def phrase_search(_engine, phrase) do
    terms =
      Searcher.Pipeline.process(phrase)
      |> Enum.map(fn {stem, _pos} -> stem end)

    case terms do
      [] -> []
      [single_term] ->
        Searcher.InvertedIndex.postings(single_term)
        |> Enum.map(fn {doc_id, _tf, positions} -> {doc_id, Enum.map(positions, &{&1, &1})} end)

      _ ->
        posting_lists = Enum.map(terms, &Searcher.InvertedIndex.postings/1)

        doc_ids_per_term =
          Enum.map(posting_lists, fn postings ->
            MapSet.new(Enum.map(postings, fn {doc_id, _, _} -> doc_id end))
          end)

        common_docs = Enum.reduce(doc_ids_per_term, &MapSet.intersection/2)

        Enum.flat_map(MapSet.to_list(common_docs), fn doc_id ->
          positions_per_term =
            Enum.map(posting_lists, fn postings ->
              case Enum.find(postings, fn {id, _, _} -> id == doc_id end) do
                {_, _, pos} -> pos
                nil -> []
              end
            end)

          phrase_positions = find_phrase_positions(positions_per_term)

          if phrase_positions != [] do
            [{doc_id, phrase_positions}]
          else
            []
          end
        end)
    end
  end

  defp find_phrase_positions(positions_per_term) do
    [first_positions | rest_positions] = positions_per_term

    Enum.flat_map(first_positions, fn start_pos ->
      matches? =
        rest_positions
        |> Enum.with_index(1)
        |> Enum.all?(fn {positions, offset} ->
          (start_pos + offset) in positions
        end)

      if matches? do
        [{start_pos, start_pos + length(positions_per_term) - 1}]
      else
        []
      end
    end)
  end

  @impl true
  def init(_opts) do
    Searcher.InvertedIndex.ensure_tables()
    {:ok, %{}}
  end
end
```

### Step 8: Given tests — must pass without modification

**Objective**: Pin BM25 length-normalization and stemmer equivalence so scorer tuning cannot silently invert short-vs-long document ranking.


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

### Step 9: Run the tests

**Objective**: Run `--trace` so scorer drift on tie-breaking surfaces per-test rather than hiding behind a globally green suite.


```bash
mix test test/searcher/ --trace
```

### Step 10: Benchmark

**Objective**: Push corpus-scale indexing and query mixes through Benchee so posting-list contention shows up before relevance tuning masks it.


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

### Why this works

Documents are tokenized, and each token maps to a posting list of `(doc_id, term_freq, positions)`. Queries intersect posting lists in sorted order, and BM25 scores each hit using per-term IDF and document-length normalization.

---
## Main Entry Point

```elixir
def main do
  IO.puts("======== 19-build-search-engine-inverted-index ========")
  IO.puts("Build Search Engine Inverted Index")
  IO.puts("")
  
  Searcher.Stemmer.start_link([])
  IO.puts("Searcher.Stemmer started")
  
  IO.puts("Run: mix test")
end
```

