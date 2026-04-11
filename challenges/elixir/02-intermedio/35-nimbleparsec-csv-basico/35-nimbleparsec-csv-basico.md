# NimbleParsec: Parser Combinators for Structured Input

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` receives job batches from partner systems via CSV files. The CSV format is non-standard: fields may be quoted, values include unicode characters, and the first line is always a header. The partner refuses to switch to JSON. You need a robust parser that handles the edge cases without regular expressions.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── csv_parser.ex           # ← you implement this
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── csv_parser_test.exs     # given tests — must pass without modification
└── mix.exs
```

Add to `mix.exs`:

```elixir
{:nimble_parsec, "~> 1.4"}
```

---

## The business problem

Partner CSV files look like this:

```
type,handler,priority
send_email,TaskQueue.Handlers.Email,high
send_sms,"TaskQueue.Handlers.SMS",low
run_report,"TaskQueue.Handlers.Report",medium
```

Edge cases the parser must handle:
- Quoted fields containing commas: `"Smith, John"`
- Quoted fields containing escaped quotes: `"He said ""hello"""`
- Unicode in field values: `"café,résumé"`
- Empty fields: `type,,priority`
- Windows line endings: `\r\n`

A regex-based parser fails on quoted commas. `String.split(",")` fails on escaped quotes. NimbleParsec composes small, testable parser pieces into a complete grammar.

---

## Why NimbleParsec and not `String.split` or regex

`String.split(line, ",")` fails for `"Smith, John",30` — the comma inside the quotes is a field separator, not a value comma. No simple string split can handle this.

Regular expressions can handle CSV, but the expression `(?:"(?:[^"\\]|\\.)*"|[^,\n]*)` is opaque. Every edge case adds another branch. The regex has no named parts — you cannot tell from reading it what `(?:[^"\\]|\\.)` means without extensive comments.

NimbleParsec composes parsers from named building blocks:

```elixir
# Readable grammar:
quoted_field   = ignore(ascii_char([?"]))
                 |> repeat(choice([escaped_quote, non_quote_char]))
                 |> ignore(ascii_char([?"]))

unquoted_field = repeat(none_of([?,, ?\n, ?\r]))

field = choice([quoted_field, unquoted_field])
row   = field |> repeat(ignore(ascii_char([?,])) |> concat(field))
```

Each combinator is testable in isolation. Adding a new edge case is adding a new combinator, not modifying a regex.

---

## Implementation

### Step 1: `lib/task_queue/csv_parser.ex`

```elixir
defmodule TaskQueue.CsvParser do
  @moduledoc """
  CSV parser for the task_queue job batch import format.

  Handles quoted fields, escaped quotes, unicode, empty fields,
  and both Unix (LF) and Windows (CRLF) line endings.

  ## Examples

      iex> TaskQueue.CsvParser.parse_line("type,handler,priority")
      {:ok, ["type", "handler", "priority"]}

      iex> TaskQueue.CsvParser.parse_line(~s("Smith, John",30))
      {:ok, ["Smith, John", "30"]}

      iex> TaskQueue.CsvParser.parse_rows("a,b\\nc,d")
      {:ok, [["a", "b"], ["c", "d"]]}

  """

  import NimbleParsec

  # An escaped quote inside a quoted field: "" represents "
  escaped_quote =
    ignore(ascii_char([?"]))
    |> ascii_char([?"])

  # Any character that is not a quote, inside a quoted field
  non_quote_char =
    utf8_char([{:not, ?"}])

  # A quoted field: "..." — returns the inner content with "" → "
  quoted_field =
    ignore(ascii_char([?"]))
    |> repeat(choice([escaped_quote, non_quote_char]))
    |> ignore(ascii_char([?"]))
    |> reduce({List, :to_string, []})

  # An unquoted field: any chars except comma, CR, LF
  unquoted_field =
    repeat(utf8_char([{:not, ?,}, {:not, ?\n}, {:not, ?\r}]))
    |> reduce({List, :to_string, []})

  # A single field: quoted or unquoted
  field = choice([quoted_field, unquoted_field])

  # A comma separator between fields
  comma = ignore(ascii_char([?,]))

  # A CSV row: field (, field)*
  # TODO: define `row` as field followed by repeat of (comma, field)
  # HINT: field |> repeat(comma |> concat(field))
  row = field |> repeat(comma |> concat(field))

  # Line ending: \r\n (Windows) or \n (Unix)
  line_ending = choice([string("\r\n"), string("\n")])

  # A complete CSV document: row (\n row)*
  # TODO: define `csv` as row followed by repeat of (line_ending, row), then optional trailing newline
  # HINT:
  # row
  # |> repeat(ignore(line_ending) |> concat(row))
  # |> ignore(optional(line_ending))
  csv = row |> repeat(ignore(line_ending) |> concat(row)) |> ignore(optional(line_ending))

  @doc false
  defparsec :parse_row_raw, row
  @doc false
  defparsec :parse_csv_raw, csv

  @doc """
  Parses a single CSV line into a list of field strings.

  Returns `{:ok, [field, ...]}` or `{:error, reason, rest}`.

  ## Examples

      iex> TaskQueue.CsvParser.parse_line("type,handler,priority")
      {:ok, ["type", "handler", "priority"]}

      iex> TaskQueue.CsvParser.parse_line(~s("Smith, John",30,"high"))
      {:ok, ["Smith, John", "30", "high"]}

  """
  @spec parse_line(String.t()) :: {:ok, [String.t()]} | {:error, term()}
  def parse_line(line) when is_binary(line) do
    case parse_row_raw(line) do
      {:ok, fields, "", _, _, _} -> {:ok, fields}
      {:ok, _, rest, _, _, _}    -> {:error, "unexpected input: #{inspect(rest)}"}
      {:error, reason, _, _, _, _} -> {:error, reason}
    end
  end

  @doc """
  Parses a multi-line CSV string into a list of rows (each row is a list of fields).

  Returns `{:ok, [[field, ...], ...]}` or `{:error, reason}`.

  ## Examples

      iex> TaskQueue.CsvParser.parse_rows("a,b\\nc,d")
      {:ok, [["a", "b"], ["c", "d"]]}

  """
  @spec parse_rows(String.t()) :: {:ok, [[String.t()]]} | {:error, term()}
  def parse_rows(input) when is_binary(input) do
    # TODO: call parse_csv_raw/1 and group flat results into rows
    # The parser returns a flat list of all fields — you need to know the row width
    # Better approach: split by line ending first, then parse each line
    #
    # HINT:
    # input
    # |> String.split(~r/\r?\n/, trim: true)
    # |> Enum.reduce_while([], fn line, acc ->
    #     case parse_line(line) do
    #       {:ok, fields} -> {:cont, [fields | acc]}
    #       {:error, _} = err -> {:halt, err}
    #     end
    #   end)
    # |> case do
    #     {:error, _} = err -> err
    #     rows -> {:ok, Enum.reverse(rows)}
    #   end
  end

  @doc """
  Parses a CSV string with a header row.

  Returns `{:ok, [%{header => value, ...}, ...]}` or `{:error, reason}`.

  ## Examples

      iex> csv = "type,handler\\nsend_email,TaskQueue.Handlers.Email"
      iex> TaskQueue.CsvParser.parse_with_headers(csv)
      {:ok, [%{"type" => "send_email", "handler" => "TaskQueue.Handlers.Email"}]}

  """
  @spec parse_with_headers(String.t()) :: {:ok, [map()]} | {:error, term()}
  def parse_with_headers(input) when is_binary(input) do
    # TODO:
    # 1. parse_rows/1 to get all rows
    # 2. First row is headers
    # 3. Zip remaining rows with headers to create maps
    # HINT:
    # with {:ok, [headers | data_rows]} <- parse_rows(input) do
    #   maps = Enum.map(data_rows, fn row -> Enum.zip(headers, row) |> Map.new() end)
    #   {:ok, maps}
    # end
  end
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/task_queue/csv_parser_test.exs
defmodule TaskQueue.CsvParserTest do
  use ExUnit.Case, async: true

  alias TaskQueue.CsvParser

  describe "parse_line/1 — single row" do
    test "simple fields without quotes" do
      assert {:ok, ["type", "handler", "priority"]} =
        CsvParser.parse_line("type,handler,priority")
    end

    test "quoted field with comma inside" do
      assert {:ok, ["Smith, John", "30"]} =
        CsvParser.parse_line(~s("Smith, John",30))
    end

    test "escaped quote inside quoted field" do
      assert {:ok, ["He said \"hello\"", "done"]} =
        CsvParser.parse_line(~s("He said ""hello""",done))
    end

    test "empty fields" do
      assert {:ok, ["type", "", "priority"]} =
        CsvParser.parse_line("type,,priority")
    end

    test "unicode characters in unquoted field" do
      assert {:ok, ["café", "résumé"]} = CsvParser.parse_line("café,résumé")
    end

    test "unicode characters in quoted field" do
      assert {:ok, ["café, naïve"]} = CsvParser.parse_line(~s("café, naïve"))
    end

    test "single field with no comma" do
      assert {:ok, ["hello"]} = CsvParser.parse_line("hello")
    end

    test "all quoted fields" do
      assert {:ok, ["a", "b", "c"]} = CsvParser.parse_line(~s("a","b","c"))
    end
  end

  describe "parse_rows/1 — multiple rows" do
    test "two rows" do
      assert {:ok, [["a", "b"], ["c", "d"]]} = CsvParser.parse_rows("a,b\nc,d")
    end

    test "windows line endings (CRLF)" do
      assert {:ok, [["a", "b"], ["c", "d"]]} = CsvParser.parse_rows("a,b\r\nc,d")
    end

    test "trailing newline is ignored" do
      assert {:ok, [["a", "b"]]} = CsvParser.parse_rows("a,b\n")
    end

    test "three rows with quoted fields" do
      csv = ~s(type,handler\nsend_email,"TaskQueue.Handlers.Email"\nsend_sms,"TaskQueue.Handlers.SMS")
      assert {:ok, rows} = CsvParser.parse_rows(csv)
      assert length(rows) == 3
    end
  end

  describe "parse_with_headers/1 — header row" do
    test "maps each row to header keys" do
      csv = "type,handler,priority\nsend_email,TaskQueue.Handlers.Email,high"
      assert {:ok, [row]} = CsvParser.parse_with_headers(csv)
      assert row["type"] == "send_email"
      assert row["handler"] == "TaskQueue.Handlers.Email"
      assert row["priority"] == "high"
    end

    test "multiple data rows" do
      csv = "type,priority\nsend_email,high\nsend_sms,low"
      assert {:ok, rows} = CsvParser.parse_with_headers(csv)
      assert length(rows) == 2
      assert hd(rows)["type"] == "send_email"
    end

    test "quoted values in data rows" do
      csv = ~s(type,handler\n"send_email","TaskQueue.Handlers.Email")
      assert {:ok, [row]} = CsvParser.parse_with_headers(csv)
      assert row["type"] == "send_email"
    end

    test "returns error for invalid CSV" do
      assert {:error, _} = CsvParser.parse_with_headers("only one field")
      # A row with fewer fields than headers is caught at the zip step
      # (no error, but map will have fewer keys — this is acceptable)
    end
  end
end
```

### Step 3: Run the tests

```bash
mix deps.get
mix test test/task_queue/csv_parser_test.exs --trace
```

---

## Trade-off analysis

| Approach | Handles quoted commas | Escaped quotes | Unicode | Maintainability |
|----------|-----------------------|---------------|---------|----------------|
| `String.split(",")` | no | no | yes | high (simple) |
| Regex | yes (complex) | yes (complex) | yes | low (opaque) |
| NimbleParsec | yes | yes | yes | high (named combinators) |
| `NimbleCSV` library | yes | yes | yes | high (drop-in) |

Reflection question: NimbleParsec generates parser code at compile time via macros. What is the practical implication: if you change the grammar in `csv_parser.ex`, must you recompile every module that calls `CsvParser.parse_line/1`, or only `csv_parser.ex` itself?

---

## Common production mistakes

**1. Not handling `{:ok, fields, rest, _, _, _}` where `rest` is non-empty**

A successful parse can still have unconsumed input. Always check that `rest == ""`:

```elixir
case parse_row_raw(line) do
  {:ok, fields, "", _, _, _} -> {:ok, fields}
  {:ok, _, rest, _, _, _}    -> {:error, "unexpected trailing input: #{inspect(rest)}"}
  {:error, msg, _, _, _, _}  -> {:error, msg}
end
```

**2. Using `string/1` combinator for multi-byte UTF-8 instead of `utf8_char/1`**

`ascii_char/1` matches single bytes. For UTF-8 multi-byte characters, use `utf8_char/1`. Using `ascii_char` on input containing `é` (2 bytes) will either fail or match each byte individually, not the codepoint.

**3. Forgetting that `repeat/1` can succeed with zero repetitions**

`repeat(field)` matches zero or more fields. If your row grammar is `repeat(field)`, an empty line parses as `{:ok, [], "", _, _, _}` — not an error. Add `min: 1` if at least one field is required: `times(field, min: 1)`.

**4. Building the grammar in function bodies instead of module-level `defparsec`**

`defparsec` generates optimized parser functions at compile time. Calling NimbleParsec combinators inside a function at runtime defeats the purpose — you must use `defparsec` or `defparsecp` at the module level.

**5. Not handling Windows line endings (`\r\n`)**

Files from Windows or partner systems often use `\r\n`. `String.split(input, "\n")` leaves a trailing `\r` on every line. Always normalize or handle both line endings explicitly in the grammar.

---

## Resources

- [NimbleParsec — official hex package](https://hexdocs.pm/nimble_parsec/NimbleParsec.html)
- [NimbleCSV — drop-in CSV library built on NimbleParsec](https://hexdocs.pm/nimble_csv/NimbleCSV.html)
- [Parser combinators explained — Saša Jurić](https://www.theerlangelist.com/article/parser_combinators)
- [CSV RFC 4180 — the standard](https://www.rfc-editor.org/rfc/rfc4180)
