# String.split, trim, and Edge Cases

**Project**: `csv_sanitizer` — standalone Mix project, 1–2 hours  
**Difficulty**: ★☆☆☆☆

---

## Project structure

```
csv_sanitizer/
├── lib/
│   └── csv_sanitizer.ex           # row + field sanitization
├── test/
│   └── csv_sanitizer_test.exs     # ExUnit tests
└── mix.exs
```

---

## What you will learn

Two core concepts:

1. **`String.split/3` with options** — the defaults do surprising things on empty strings
   and trailing separators. `:trim`, `:parts`, regex patterns, and list-of-patterns each
   solve a different problem.
2. **Idiomatic trimming** — `String.trim/1`, `String.trim_leading/2`, `String.trim_trailing/2`,
   and why you almost never need a regex for whitespace cleanup.

---

## The business problem

You ingest CSVs from third parties. The files are correctly-formatted-ish, but:

- Rows sometimes have trailing commas (`"a,b,c,"`) — should produce 4 fields, not 3.
- Fields arrive with leading/trailing whitespace and/or non-breaking spaces (`\u00A0`).
- Some cells contain a literal `NULL` string that should become `nil`.
- Some rows are blank or only whitespace — they must be skipped, not parsed as a 1-field row.

You need a robust sanitizer that runs before the real CSV logic. Think "defensive
normalization layer", not a full RFC 4180 parser (use a library for that).

---

## Why `String.split/3` trips people up

Three defaults bite:

1. `String.split("", ",")` returns `[""]`, not `[]`. An empty row looks like one empty cell.
2. `String.split("a,b,", ",")` returns `["a", "b", ""]` — the trailing empty IS a field.
3. `String.split("  hello  ", " ")` returns `["", "", "hello", "", ""]`. Splitting on
   single-space and expecting whitespace-agnostic behavior is wrong — use a regex or `trim`.

The fix is almost always `trim: true` (drops empty segments) or splitting on a regex pattern.
Know which you want.

---

## Why `String.trim/1` and not `String.replace(/\s+/, "")`

`String.trim/1` is Unicode-aware: it strips non-breaking space (`\u00A0`), thin space,
ideographic space — everything Unicode classifies as whitespace. A naive regex `\s` in
Erlang's re library is ASCII-only by default. If you copy-paste text from Word or a
web page, NBSP slips past `\s` and your "trim" silently does nothing.

---

## Implementation

### Step 1 — Create the project

```bash
mix new csv_sanitizer
cd csv_sanitizer
```

### Step 2 — `lib/csv_sanitizer.ex`

```elixir
defmodule CsvSanitizer do
  @moduledoc """
  Pre-parse sanitization for noisy CSV input.
  """

  @doc """
  Parses a single CSV row into a list of sanitized fields (or `nil` for NULL sentinels).

  - Returns `[]` for an empty or whitespace-only row — the caller decides whether to skip.
  - Preserves trailing empty fields (`"a,b,"` → `["a", "b", nil]`) because they ARE
    semantically present in CSV.
  - Trims each field using the Unicode-aware `String.trim/1`.
  """
  @spec parse_row(String.t()) :: [String.t() | nil]
  def parse_row(row) when is_binary(row) do
    # First decision: is the row empty/whitespace-only?
    # We can't rely on `String.split("", ",") == [""]` — that would become [nil].
    case String.trim(row) do
      "" -> []
      _nonblank -> row |> String.split(",") |> Enum.map(&sanitize_field/1)
    end
  end

  @doc """
  Parses a full CSV document, dropping blank rows.

  Splits on any line-ending variant (`\\n`, `\\r\\n`, `\\r`) via a regex pattern —
  CRLF files from Windows and bare-CR files from classic macOS both work.
  """
  @spec parse_document(String.t()) :: [[String.t() | nil]]
  def parse_document(doc) when is_binary(doc) do
    doc
    |> String.split(~r/\r\n|\r|\n/)
    |> Enum.map(&parse_row/1)
    |> Enum.reject(&(&1 == []))
  end

  # ---- internals -------------------------------------------------------------

  # "NULL" (case-insensitive) and "" both become nil — the two most common sentinels
  # third parties use to mean "absent". Anything else is kept as a trimmed string.
  defp sanitize_field(field) do
    case field |> String.trim() |> String.downcase() do
      "" -> nil
      "null" -> nil
      _ -> String.trim(field)
    end
  end
end
```

### Step 3 — `test/csv_sanitizer_test.exs`

```elixir
defmodule CsvSanitizerTest do
  use ExUnit.Case, async: true

  describe "parse_row/1" do
    test "splits a simple row" do
      assert CsvSanitizer.parse_row("a,b,c") == ["a", "b", "c"]
    end

    test "trims whitespace around fields" do
      assert CsvSanitizer.parse_row("  a , b  ,  c") == ["a", "b", "c"]
    end

    test "preserves trailing empty field" do
      assert CsvSanitizer.parse_row("a,b,") == ["a", "b", nil]
    end

    test "converts literal NULL to nil (case-insensitive)" do
      assert CsvSanitizer.parse_row("a,NULL,null,Null") == ["a", nil, nil, nil]
    end

    test "returns [] for empty row" do
      assert CsvSanitizer.parse_row("") == []
    end

    test "returns [] for whitespace-only row" do
      assert CsvSanitizer.parse_row("   \t  ") == []
    end

    test "handles Unicode non-breaking space in trim" do
      # \u00A0 is NBSP — a naive \s regex would miss it.
      assert CsvSanitizer.parse_row("\u00A0hello\u00A0,\u00A0world\u00A0") ==
               ["hello", "world"]
    end
  end

  describe "parse_document/1" do
    test "splits on LF, CRLF, and bare CR" do
      doc = "a,b\r\nc,d\ne,f\rg,h"
      assert CsvSanitizer.parse_document(doc) == [
               ["a", "b"],
               ["c", "d"],
               ["e", "f"],
               ["g", "h"]
             ]
    end

    test "drops blank lines" do
      doc = "a,b\n\n   \nc,d\n"
      assert CsvSanitizer.parse_document(doc) == [["a", "b"], ["c", "d"]]
    end
  end
end
```

### Step 4 — Run the tests

```bash
mix test
```

All 9 tests should pass.

---

## Trade-offs

| Approach | Best for | Pitfall |
|----------|----------|---------|
| `String.split(s, ",")` | Fixed-delimiter splits, keeping empties | Trailing empty is kept — know if you want that |
| `String.split(s, ",", trim: true)` | Filtering out empties | Silently drops semantically meaningful empties |
| `String.split(s, ~r/\s+/)` | Whitespace-agnostic splits | Slower than single-char split; regex compile cost |
| `String.split(s, [" ", "\t", "\n"])` | Small, explicit delimiter set | Verbose; still misses NBSP and other Unicode spaces |
| `String.splitter/3` (lazy) | Very large strings | Lazy — combine with `Enum.take/2` to avoid full scan |

---

## Common production mistakes

**1. Forgetting that `String.split/2` keeps trailing empty strings**  
`"a,b,".split(",")` is `["a", "b", ""]`. If you iterate expecting 2 fields, you get 3.
Either add `trim: true` (if empties are noise) or handle the empty explicitly.

**2. Splitting on `"\n"` only**  
Files from Windows have `\r\n`. You end up with stray `\r` at the end of every line,
breaking equality checks downstream. Split on `~r/\r\n|\r|\n/` or call
`String.replace(s, "\r\n", "\n")` first.

**3. Trim with a regex instead of `String.trim/1`**  
`Regex.replace(~r/^\s+|\s+$/, s, "")` looks equivalent but misses Unicode whitespace
classes unless you use the `u` flag. `String.trim/1` is shorter, faster, and correct.

**4. Using `String.split/2` for very large strings without `:parts` or `String.splitter/3`**  
`String.split/2` materializes the full list. For a 100MB string you allocate the whole list
of fields at once. Prefer `String.splitter/3` (lazy stream) for files loaded whole.

---

## When NOT to roll your own

For any real CSV input (quoted fields, embedded commas, escaped quotes), use
[`nimble_csv`](https://hex.pm/packages/nimble_csv) or
[`csv`](https://hex.pm/packages/csv). This sanitizer is a preprocessing layer, not a parser.
The moment you reach for quote handling, stop and reach for the library instead.

---

## Resources

- [`String.split/3` docs](https://hexdocs.pm/elixir/String.html#split/3)
- [`String.splitter/3` for lazy splitting](https://hexdocs.pm/elixir/String.html#splitter/3)
- [`String.trim/1` vs regex](https://hexdocs.pm/elixir/String.html#trim/1)
- [`NimbleCSV`](https://hexdocs.pm/nimble_csv) — for when you outgrow this
