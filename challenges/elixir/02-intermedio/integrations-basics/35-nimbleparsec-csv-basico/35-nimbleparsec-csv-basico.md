# Parsing CSV with NimbleParsec (quotes, escapes, and all)

**Project**: `csv_parser_nimble` — a hand-built CSV parser using `NimbleParsec` combinators, including the RFC-4180 double-quote escaping rule.

**Difficulty**: ★★★☆☆
**Estimated time**: 3–4 hours

---

## Project context

CSV looks trivial until you read RFC 4180 and realize it isn't: fields can
be quoted; quoted fields can contain commas, newlines, and literal double
quotes escaped as `""`. `String.split/2` does not handle any of that. A
regex technically can, but it's unreadable and slow on big files.

`NimbleParsec` is Dashbit's parser-combinator library: you compose tiny
parsers (`ascii_char`, `string`, `choice`, `repeat`) into bigger ones,
and at compile time it generates an efficient recursive-descent parser.
It's what Phoenix, Calendar, and Floki use for their grammars. Writing a
CSV parser by hand is the canonical first NimbleParsec exercise because
the grammar is small but has enough subtleties (escaping!) to matter.

Project structure:

```
csv_parser_nimble/
├── lib/
│   └── csv_parser_nimble.ex
├── test/
│   └── csv_parser_nimble_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Parser-combinators as a pipeline

```elixir
defparsec :field, choice([quoted_field, unquoted_field])
```

`defparsec/2` compiles a parser into a function you can call with a
binary input. It returns `{:ok, results, rest, context, line, column}` or
`{:error, reason, rest, context, line, column}`. You compose a grammar by
naming sub-parsers with `defparsecp` (private) and gluing them together
with `choice`, `concat`, `repeat`, `optional`, `times`.

### 2. `utf8_char` vs `ascii_char`

`ascii_char([not: ?,])` — matches one ASCII byte that is not a comma.
`utf8_char([not: ?,])` — matches one UTF-8 codepoint that is not a comma.

CSV data is often not pure ASCII (accents, emoji). Use `utf8_char` for
text fields. Use `ascii_char` for structural punctuation (commas, quotes,
newlines) because they're always single-byte.

### 3. The escaped-quote rule

In a quoted field, a literal `"` is written as `""`. So `"He said ""hi"""`
parses to the string `He said "hi"`. The trick is to express this as
"either any non-quote codepoint, or a two-char sequence `""` that emits
a single `"`".

```
quoted_field = `"` ( not_quote | `""`→`"` )* `"`
```

### 4. `reduce/3` — post-process match results into a single value

A parser builds a list of matched tokens. `reduce/3` lets you fold them
into one term — e.g. a list of chars into a binary via `List.to_string/1`.
This keeps the parser output tidy without a post-walk.

### 5. Compile-time vs runtime

`defparsec` generates code at compile time. That means grammar changes
require a recompile; it also means the parser is as fast as hand-written
Elixir pattern matching (which is what it compiles to). This is why
NimbleParsec is preferred over runtime parser-combinator libraries for
hot-path code.

---

## Implementation

### Step 1: Create the project

```bash
mix new csv_parser_nimble
cd csv_parser_nimble
```

Add `:nimble_parsec` in `mix.exs`:

```elixir
defp deps do
  [
    {:nimble_parsec, "~> 1.4"}
  ]
end
```

Run `mix deps.get`.

### Step 2: `lib/csv_parser_nimble.ex`

```elixir
defmodule CsvParserNimble do
  @moduledoc """
  A small, RFC-4180-aware CSV parser built with NimbleParsec.

  Supported:
    * Comma separator, LF or CRLF line terminators.
    * Quoted fields with embedded commas, CRs, LFs, and `""`-escaped quotes.
    * UTF-8 field contents.

  Not supported (intentional, to keep the grammar teachable):
    * Configurable separator (always `,`).
    * Streaming — `parse/1` expects the whole document in memory.
    * Header extraction — callers can `[headers | rows]` themselves.
  """

  import NimbleParsec

  # --- Character classes ----------------------------------------------------
  # Structural bytes are always ASCII, so `ascii_char` is fine here.
  comma = ascii_char([?,])
  dquote = ascii_char([?"])

  # End-of-line is LF or CRLF. Match CRLF first so CR doesn't get swallowed
  # by something else (like an unquoted char) before we see the LF.
  eol = choice([string("\r\n"), string("\n")]) |> replace(:eol)

  # --- Unquoted field -------------------------------------------------------
  # Anything except comma, CR, LF, or double-quote. UTF-8 codepoints allowed.
  unquoted_char = utf8_char([{:not, ?,}, {:not, ?\r}, {:not, ?\n}, {:not, ?"}])

  unquoted_field =
    repeat(unquoted_char)
    |> reduce({List, :to_string, []})
    |> unwrap_and_tag(:field)

  # --- Quoted field ---------------------------------------------------------
  # Inside a quoted field:
  #   - any codepoint that isn't `"` is literal, OR
  #   - `""` is a single literal `"`.
  escaped_quote = string(~s("")) |> replace(?")
  quoted_char = choice([utf8_char([{:not, ?"}]), escaped_quote])

  quoted_field =
    ignore(dquote)
    |> repeat(quoted_char)
    |> ignore(dquote)
    |> reduce({List, :to_string, []})
    |> unwrap_and_tag(:field)

  # A field is quoted OR unquoted. Order matters: try quoted first, because
  # an unquoted field happily matches zero chars and would beat quoted to it.
  field = choice([quoted_field, unquoted_field])

  # --- Record (one line of N fields) ---------------------------------------
  # A record is: field, (`,` field)*.
  record =
    field
    |> repeat(ignore(comma) |> concat(field))
    |> tag(:record)

  # --- Document -------------------------------------------------------------
  # A document is one or more records separated by EOLs, with an optional
  # trailing EOL.
  document =
    record
    |> repeat(ignore(eol) |> concat(record))
    |> optional(ignore(eol))

  defparsec :parse_document, document

  # --- Public API -----------------------------------------------------------

  @doc """
  Parses a CSV document into a list of rows. Each row is a list of strings.

  ## Examples

      iex> CsvParserNimble.parse("a,b,c\\n1,\\"2,2\\",3\\n")
      {:ok, [["a", "b", "c"], ["1", "2,2", "3"]]}
  """
  @spec parse(binary()) :: {:ok, [[String.t()]]} | {:error, String.t()}
  def parse(input) when is_binary(input) do
    case parse_document(input) do
      {:ok, tagged, "", _context, _line, _col} ->
        {:ok, Enum.map(tagged, &extract_record/1)}

      {:ok, _, rest, _, line, col} ->
        {:error, "trailing unparsed input at line #{line}, column #{col}: #{inspect(rest)}"}

      {:error, reason, rest, _, line, col} ->
        {:error, "#{reason} at line #{line}, column #{col}: #{inspect(rest)}"}
    end
  end

  # Each record comes back as `{:record, [{:field, "a"}, {:field, "b"}, ...]}`.
  defp extract_record({:record, fields}), do: Enum.map(fields, fn {:field, v} -> v end)
end
```

### Step 3: `test/csv_parser_nimble_test.exs`

```elixir
defmodule CsvParserNimbleTest do
  use ExUnit.Case, async: true
  doctest CsvParserNimble

  describe "parse/1 — unquoted fields" do
    test "single row, three fields" do
      assert {:ok, [["a", "b", "c"]]} = CsvParserNimble.parse("a,b,c")
    end

    test "empty fields are preserved" do
      assert {:ok, [["a", "", "c"]]} = CsvParserNimble.parse("a,,c")
    end

    test "multiple rows separated by LF" do
      assert {:ok, [["1", "2"], ["3", "4"]]} = CsvParserNimble.parse("1,2\n3,4")
    end

    test "multiple rows separated by CRLF" do
      assert {:ok, [["1", "2"], ["3", "4"]]} = CsvParserNimble.parse("1,2\r\n3,4")
    end

    test "trailing newline is accepted" do
      assert {:ok, [["x"]]} = CsvParserNimble.parse("x\n")
    end
  end

  describe "parse/1 — quoted fields" do
    test "embedded commas don't split the field" do
      assert {:ok, [["a", "b,c", "d"]]} = CsvParserNimble.parse(~s(a,"b,c",d))
    end

    test "embedded newlines are preserved" do
      assert {:ok, [["a", "line1\nline2", "z"]]} =
               CsvParserNimble.parse(~s(a,"line1\nline2",z))
    end

    test ~s("" inside a quoted field becomes a literal ") do
      assert {:ok, [["a", ~s(he said "hi"), "z"]]} =
               CsvParserNimble.parse(~s(a,"he said ""hi""",z))
    end

    test "empty quoted field" do
      assert {:ok, [["a", "", "z"]]} = CsvParserNimble.parse(~s(a,"",z))
    end
  end

  describe "parse/1 — UTF-8" do
    test "accepts multi-byte characters in unquoted fields" do
      assert {:ok, [["café", "niño", "🚀"]]} = CsvParserNimble.parse("café,niño,🚀")
    end

    test "accepts multi-byte characters in quoted fields" do
      assert {:ok, [["a", "día, soleado", "z"]]} =
               CsvParserNimble.parse(~s(a,"día, soleado",z))
    end
  end

  describe "parse/1 — errors" do
    test "unterminated quoted field is reported" do
      assert {:error, reason} = CsvParserNimble.parse(~s(a,"unterminated,z))
      assert reason =~ "line"
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. NimbleParsec is compile-time only**
Your grammar is frozen at compile. If you need a user-configurable
separator (`;` or `\t`), either generate multiple parsers at compile time
(one per separator) or use a runtime solution like `NimbleCSV`'s
pre-built parsers. Do not rebuild the parser at runtime — you'd lose
the speed that justified NimbleParsec.

**2. This parser is non-streaming**
`parse/1` takes the full binary. For gigabyte CSVs you want `NimbleCSV`
(same authors) which is line-oriented and streams. The grammar here is
for learning; `NimbleCSV` is the library you ship.

**3. Error messages are positional, not semantic**
NimbleParsec reports "expected X" at a line/column, not "you forgot to
close a quote". Good enough for developer-facing errors, wrong tool for
showing messages to end users. Wrap `{:error, _}` with your own
domain-level diagnostics if users see them.

**4. Beware `utf8_char` in hot loops**
UTF-8 decoding is more expensive than byte matching. If your CSV is
known-ASCII (many machine-generated files are), swapping to `ascii_char`
gives a measurable speedup.

**5. The grammar deliberately rejects quotes in unquoted fields**
`"hello"world` or `hel"lo` are not valid RFC-4180. This parser will
fail on them (the quote puts it into quoted mode). If your real-world
input is dirty, you'll need a permissive mode — and you'll cry.

**6. When NOT to use NimbleParsec**
- For CSV specifically, use `NimbleCSV` — faster, streaming, battle-tested.
- For JSON, use `Jason`. Never hand-roll.
- For "I just need to split on commas", use `String.split/2`.
- NimbleParsec shines when you own a DSL or a format with no library:
  query languages, config files, protocol headers.

---

## Resources

- [`NimbleParsec` — hexdocs](https://hexdocs.pm/nimble_parsec/)
- [Dashbit blog — NimbleParsec announcements and deep dives](https://dashbit.co/blog/)
- [`NimbleCSV`](https://hexdocs.pm/nimble_csv/) — the production-grade CSV parser you'd actually ship
- [RFC 4180 — the CSV spec](https://datatracker.ietf.org/doc/html/rfc4180)
- [`NimbleParsec.Helpers` — combinator cheat sheet](https://hexdocs.pm/nimble_parsec/NimbleParsec.html#functions)
