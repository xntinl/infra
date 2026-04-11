# NimbleParsec: Parser Combinators for Structured Input

## Goal

Build a CSV parser for `task_queue` job batch imports using NimbleParsec. Handle quoted fields, escaped quotes, Unicode characters, empty fields, and both Unix and Windows line endings. Learn why parser combinators are more maintainable than regex for structured formats.

---

## Why NimbleParsec and not `String.split` or regex

`String.split(line, ",")` fails for `"Smith, John",30` -- the comma inside the quotes is a field separator, not a value comma.

Regular expressions can handle CSV, but the expression is opaque. Every edge case adds another branch.

NimbleParsec composes parsers from named building blocks. Each combinator is testable in isolation. Adding a new edge case is adding a new combinator, not modifying a regex. NimbleParsec also generates parser code at compile time via macros, so performance is excellent.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [
      {:nimble_parsec, "~> 1.4"}
    ]
  end
end
```

### Step 2: `lib/task_queue/csv_parser.ex`

The parser is built from small composable pieces:

- `escaped_quote`: inside a quoted field, `""` represents a literal `"` character
- `non_quote_char`: any UTF-8 character that is not `"`
- `quoted_field`: content between `"..."`, with `""` unescaped to `"`
- `unquoted_field`: any characters except `,`, `\n`, `\r`
- `field`: either a quoted or unquoted field
- `row`: a field followed by zero or more `, field` pairs
- `line_ending`: `\r\n` (Windows) or `\n` (Unix)

The `reduce({List, :to_string, []})` combinator collects the matched codepoints into a single string. The `ignore/1` combinator drops delimiters (quotes, commas, newlines) from the output.

`parse_rows/1` splits the input by line endings first, then parses each line individually. This is simpler than trying to track row boundaries in a flat list of fields from the parser.

`parse_with_headers/1` treats the first row as header keys and zips them with each subsequent row to produce maps.

```elixir
defmodule TaskQueue.CsvParser do
  @moduledoc """
  CSV parser for the task_queue job batch import format.

  Handles quoted fields, escaped quotes, Unicode, empty fields,
  and both Unix (LF) and Windows (CRLF) line endings.
  """

  import NimbleParsec

  escaped_quote =
    ignore(ascii_char([?"]))
    |> ascii_char([?"])

  non_quote_char =
    utf8_char([{:not, ?"}])

  quoted_field =
    ignore(ascii_char([?"]))
    |> repeat(choice([escaped_quote, non_quote_char]))
    |> ignore(ascii_char([?"]))
    |> reduce({List, :to_string, []})

  unquoted_field =
    repeat(utf8_char([{:not, ?,}, {:not, ?\n}, {:not, ?\r}]))
    |> reduce({List, :to_string, []})

  field = choice([quoted_field, unquoted_field])

  comma = ignore(ascii_char([?,]))

  row = field |> repeat(comma |> concat(field))

  line_ending = choice([string("\r\n"), string("\n")])

  csv =
    row
    |> repeat(ignore(line_ending) |> concat(row))
    |> ignore(optional(line_ending))

  @doc false
  defparsec :parse_row_raw, row
  @doc false
  defparsec :parse_csv_raw, csv

  @doc """
  Parses a single CSV line into a list of field strings.

  Returns `{:ok, [field, ...]}` or `{:error, reason}`.

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
  Parses a multi-line CSV string into a list of rows.

  Returns `{:ok, [[field, ...], ...]}` or `{:error, reason}`.

  ## Examples

      iex> TaskQueue.CsvParser.parse_rows("a,b\\nc,d")
      {:ok, [["a", "b"], ["c", "d"]]}

  """
  @spec parse_rows(String.t()) :: {:ok, [[String.t()]]} | {:error, term()}
  def parse_rows(input) when is_binary(input) do
    input
    |> String.split(~r/\r?\n/, trim: true)
    |> Enum.reduce_while([], fn line, acc ->
      case parse_line(line) do
        {:ok, fields} -> {:cont, [fields | acc]}
        {:error, _} = err -> {:halt, err}
      end
    end)
    |> case do
      {:error, _} = err -> err
      rows -> {:ok, Enum.reverse(rows)}
    end
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
    with {:ok, [headers | data_rows]} <- parse_rows(input) do
      maps = Enum.map(data_rows, fn row -> Enum.zip(headers, row) |> Map.new() end)
      {:ok, maps}
    end
  end
end
```

### Step 3: Tests

```elixir
# test/task_queue/csv_parser_test.exs
defmodule TaskQueue.CsvParserTest do
  use ExUnit.Case, async: true

  alias TaskQueue.CsvParser

  describe "parse_line/1 -- single row" do
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
      assert {:ok, ["cafe", "resume"]} = CsvParser.parse_line("cafe,resume")
    end

    test "unicode characters in quoted field" do
      assert {:ok, ["cafe, naive"]} = CsvParser.parse_line(~s("cafe, naive"))
    end

    test "single field with no comma" do
      assert {:ok, ["hello"]} = CsvParser.parse_line("hello")
    end

    test "all quoted fields" do
      assert {:ok, ["a", "b", "c"]} = CsvParser.parse_line(~s("a","b","c"))
    end
  end

  describe "parse_rows/1 -- multiple rows" do
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

  describe "parse_with_headers/1 -- header row" do
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
    end
  end
end
```

### Step 4: Run

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

NimbleParsec generates parser code at compile time. If you change the grammar in `csv_parser.ex`, only `csv_parser.ex` itself needs recompilation -- modules that call `CsvParser.parse_line/1` do not change.

---

## Common production mistakes

**1. Not handling `{:ok, fields, rest, _, _, _}` where `rest` is non-empty**
A successful parse can still have unconsumed input. Always check `rest == ""`.

**2. Using `ascii_char/1` for multi-byte UTF-8 instead of `utf8_char/1`**
`ascii_char/1` matches single bytes. For UTF-8 characters, use `utf8_char/1`.

**3. Forgetting that `repeat/1` can succeed with zero repetitions**
`repeat(field)` matches zero or more. Use `times(field, min: 1)` if at least one is required.

**4. Building the grammar in function bodies instead of module-level `defparsec`**
`defparsec` generates optimized parser functions at compile time. Calling combinators at runtime defeats the purpose.

**5. Not handling Windows line endings (`\r\n`)**
Files from Windows use `\r\n`. `String.split(input, "\n")` leaves a trailing `\r` on every line.

---

## Resources

- [NimbleParsec -- official hex package](https://hexdocs.pm/nimble_parsec/NimbleParsec.html)
- [NimbleCSV -- drop-in CSV library built on NimbleParsec](https://hexdocs.pm/nimble_csv/NimbleCSV.html)
- [Parser combinators explained -- Sasa Juric](https://www.theerlangelist.com/article/parser_combinators)
- [CSV RFC 4180 -- the standard](https://www.rfc-editor.org/rfc/rfc4180)
