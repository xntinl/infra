# Strings and Binaries: Parsing CSV Transaction Lines

**Project**: `payments_cli` — a CLI tool that processes payment transactions

---

## Project context

You are building `payments_cli`, a CLI tool that processes payment transactions from CSV
files, validates them, applies business rules, and produces ledger reports.

This exercise implements a `Formatter` module that parses raw CSV lines into structured
data, truncates merchant names respecting UTF-8 grapheme boundaries, normalizes reference
IDs, and validates string integrity. These operations are fundamental to processing
bank-exported CSV files that may contain non-ASCII merchant names.

---

## Why strings as binaries matters in a payments context

A CSV file exported by a European bank may contain merchant names like
`"Cafe Munchen GmbH"`. If you treat strings as byte arrays (as C does),
the length of that name is 19 bytes but only 16 visible characters.
Truncating to 15 "characters" by bytes splits `u` in the middle and
produces invalid UTF-8 — data corruption.

Elixir strings are UTF-8 encoded binaries. The distinction matters:
- `byte_size("Cafe")` -> `5` (bytes, O(1), used for binary protocol headers)
- `String.length("Cafe")` -> `4` (graphemes, O(n), used for display truncation)
- `String.valid?/1` -> validates UTF-8 before storing or forwarding data

The other gotcha: bank-exported CSVs often arrive with Erlang charlists from
old Erlang library integrations. A senior developer recognizes `'hello'` (charlist)
vs `"hello"` (binary) and knows when `to_string/1` is needed.

---

## The business problem

The `Formatter` module needs to:

1. Parse a raw CSV line into a map of field values
2. Truncate merchant names to a display length (respecting UTF-8 graphemes)
3. Normalize reference IDs (uppercase, trim, remove internal spaces)
4. Validate that a string is non-empty and valid UTF-8

---

## Implementation

### `lib/payments_cli/formatter.ex`

The CSV parser splits on commas, trims whitespace, and validates the field count.
Amount parsing uses `Integer.parse/1` instead of `String.to_integer/1` because the
latter raises on invalid input — dangerous in a parser that processes thousands of rows.

Truncation uses `String.length/1` and `String.slice/3` (grapheme-aware operations)
instead of `byte_size` and binary slicing. This ensures multi-byte characters like
`e` and `u` are never split mid-byte.

```elixir
defmodule PaymentsCli.Formatter do
  @moduledoc """
  Parses and formats transaction data for display and storage.

  All string operations use the String module (not binary/charlist operations)
  to correctly handle UTF-8 merchant names and reference fields.
  """

  @doc """
  Parses a CSV line into a map with typed values.

  Expected CSV format: "id,amount_cents,currency,merchant,status"

  Returns {:ok, map} or {:error, reason}.

  ## Examples

      iex> PaymentsCli.Formatter.parse_csv_line("TXN001,1234,USD,Coffee Shop,approved")
      {:ok, %{id: "TXN001", amount_cents: 1234, currency: "USD", merchant: "Coffee Shop", status: "approved"}}

      iex> PaymentsCli.Formatter.parse_csv_line("bad data")
      {:error, "expected 5 fields, got 1"}

  """
  @spec parse_csv_line(String.t()) :: {:ok, map()} | {:error, String.t()}
  def parse_csv_line(line) when is_binary(line) do
    fields = line |> String.split(",") |> Enum.map(&String.trim/1)

    case fields do
      [id, amount_str, currency, merchant, status] ->
        case Integer.parse(amount_str) do
          {amount, ""} ->
            {:ok, %{
              id: id,
              amount_cents: amount,
              currency: currency,
              merchant: merchant,
              status: status
            }}

          _ ->
            {:error, "invalid amount: #{amount_str}"}
        end

      _ ->
        {:error, "expected 5 fields, got #{length(fields)}"}
    end
  end

  @doc """
  Truncates a merchant name to max_length graphemes, adding "..." if truncated.

  Uses String.length/1 and String.slice/3 — NOT byte_size — so UTF-8 merchant
  names like "Cafe Munchen" are truncated at grapheme boundaries.

  ## Examples

      iex> PaymentsCli.Formatter.truncate_merchant("Coffee Shop", 20)
      "Coffee Shop"

      iex> PaymentsCli.Formatter.truncate_merchant("A Very Long Merchant Name Here", 15)
      "A Very Long Mer..."

      iex> PaymentsCli.Formatter.truncate_merchant("Cafe Munchen GmbH", 10)
      "Cafe Munch..."

  """
  @spec truncate_merchant(String.t(), pos_integer()) :: String.t()
  def truncate_merchant(name, max_length)
      when is_binary(name) and is_integer(max_length) and max_length > 0 do
    if String.length(name) <= max_length do
      name
    else
      String.slice(name, 0, max_length - 1) <> "\u2026"
    end
  end

  @doc """
  Normalizes a transaction reference ID from external input.

  Rules: uppercase, trim whitespace, remove internal spaces.

  ## Examples

      iex> PaymentsCli.Formatter.normalize_reference("  txn 001 abc  ")
      "TXN001ABC"

  """
  @spec normalize_reference(String.t()) :: String.t()
  def normalize_reference(ref) when is_binary(ref) do
    ref
    |> String.trim()
    |> String.upcase()
    |> String.replace(" ", "")
  end

  @doc """
  Validates that a string is non-empty and valid UTF-8.

  Returns {:ok, string} or {:error, reason}.

  ## Examples

      iex> PaymentsCli.Formatter.validate_string("hello")
      {:ok, "hello"}

      iex> PaymentsCli.Formatter.validate_string("")
      {:error, "string is empty"}

  """
  @spec validate_string(String.t()) :: {:ok, String.t()} | {:error, String.t()}
  def validate_string(value) when is_binary(value) do
    cond do
      not String.valid?(value) ->
        {:error, "invalid UTF-8"}

      byte_size(value) == 0 ->
        {:error, "string is empty"}

      true ->
        {:ok, value}
    end
  end
end
```

**Why this works:**

- `parse_csv_line/1` splits on commas, trims each field, then pattern matches on exactly
  five elements. If the split produces more or fewer fields, the catch-all returns an
  error with the actual count. `Integer.parse/1` returns `{integer, rest}` on success
  or `:error` — matching on `{amount, ""}` ensures the entire string was consumed
  (no trailing characters like `"123abc"`).

- `truncate_merchant/2` checks `String.length/1` (grapheme count, not byte count) before
  deciding to truncate. If truncation is needed, it slices to `max_length - 1` graphemes
  and appends the ellipsis `"\u2026"` (a single grapheme, 3 bytes in UTF-8). The total
  grapheme count of the result equals `max_length`.

- `normalize_reference/1` is a natural pipeline: trim -> upcase -> remove spaces. Each
  step transforms the string and passes the result to the next via `|>`.

- `validate_string/1` checks UTF-8 validity first with `String.valid?/1`, then checks
  for empty. Order matters: calling `String.length/1` on invalid UTF-8 could raise.
  We use `byte_size(value) == 0` for the empty check because it is O(1) and safe
  even on invalid binaries.

### Tests

```elixir
# test/payments_cli/formatter_test.exs
defmodule PaymentsCli.FormatterTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Formatter

  describe "parse_csv_line/1" do
    test "parses a valid CSV line" do
      assert {:ok, tx} = Formatter.parse_csv_line("TXN001,1234,USD,Coffee Shop,approved")
      assert tx.id == "TXN001"
      assert tx.amount_cents == 1234
      assert tx.currency == "USD"
      assert tx.merchant == "Coffee Shop"
      assert tx.status == "approved"
    end

    test "trims whitespace from fields" do
      assert {:ok, tx} = Formatter.parse_csv_line(" TXN002 , 500 , EUR , Cafe , pending ")
      assert tx.id == "TXN002"
      assert tx.amount_cents == 500
      assert tx.merchant == "Cafe"
    end

    test "returns error for wrong field count" do
      assert {:error, message} = Formatter.parse_csv_line("bad data")
      assert is_binary(message)
    end

    test "returns error for non-integer amount" do
      assert {:error, _} = Formatter.parse_csv_line("TXN003,not_a_number,USD,Shop,approved")
    end
  end

  describe "truncate_merchant/2" do
    test "returns name unchanged when within limit" do
      assert Formatter.truncate_merchant("Coffee Shop", 20) == "Coffee Shop"
    end

    test "truncates at grapheme boundary and adds ellipsis" do
      result = Formatter.truncate_merchant("A Very Long Merchant Name Here", 15)
      assert String.length(result) == 15
      assert String.ends_with?(result, "\u2026")
    end

    test "handles UTF-8 merchant names correctly" do
      # "Cafe Munchen" has 12 graphemes but 14 bytes
      result = Formatter.truncate_merchant("Cafe Munchen GmbH", 10)
      assert String.length(result) == 10
      # Verify UTF-8 is still valid after truncation
      assert String.valid?(result)
    end
  end

  describe "normalize_reference/1" do
    test "uppercases and removes spaces" do
      assert Formatter.normalize_reference("  txn 001 abc  ") == "TXN001ABC"
    end

    test "handles already normalized input" do
      assert Formatter.normalize_reference("TXN001") == "TXN001"
    end
  end

  describe "validate_string/1" do
    test "returns ok for valid string" do
      assert {:ok, "hello"} = Formatter.validate_string("hello")
    end

    test "returns error for empty string" do
      assert {:error, _} = Formatter.validate_string("")
    end

    test "returns error for invalid UTF-8" do
      assert {:error, _} = Formatter.validate_string(<<0xFF, 0xFE>>)
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/formatter_test.exs --trace
```

---

## Trade-off analysis

| Aspect | String module (your impl) | Binary pattern matching | Regex |
|--------|--------------------------|------------------------|-------|
| UTF-8 correctness | Automatic | Manual byte handling needed | Depends on flag |
| Performance | O(n) per operation | O(n) but lower constant | Higher overhead |
| CSV parsing | Simple split + trim | Requires delimiter handling | Overkill for simple CSV |
| Truncation | Grapheme-safe with `slice` | Byte-level, can corrupt | Not applicable |

Reflection question: `parse_csv_line/1` uses `String.split(line, ",")`. What happens
if a merchant name contains a comma, like `"Smith, Jones Ltd"`? How would you fix
the parser to handle quoted CSV fields?

---

## Common production mistakes

**1. Using `byte_size` for display truncation**
`byte_size("Cafe Munchen")` returns `14`, not `12`. Truncating by bytes
instead of `String.length` + `String.slice` corrupts multi-byte characters
and produces invalid UTF-8 that downstream systems reject.

**2. Charlist vs binary confusion from Erlang libraries**
Some Erlang HTTP clients and file libraries return charlists (`'hello'`) instead
of binaries (`"hello"`). `String.upcase('hello')` raises `FunctionClauseError`.
Wrap Erlang library calls with `to_string/1` or `List.to_string/1` at the boundary.

**3. `String.to_integer/1` raises on invalid input**
`String.to_integer("abc")` raises `ArgumentError`. In a CSV parser that processes
thousands of rows, one bad row kills the process. Use `Integer.parse/1` which
returns `:error` instead of raising.

**4. String concatenation in a loop with `<>`**
Building a report string with `acc <> line` in each iteration is O(n^2) — each
`<>` creates a new binary by copying. Use `IO.iodata_to_binary/1` with an iolist,
or `Enum.join/2`, to build strings efficiently.

**5. Forgetting `String.trim/1` on CSV fields**
Bank CSV exports often have trailing spaces or Windows line endings (`\r\n`).
Always trim fields after splitting. `"approved\r"` does not match `"approved"`.

---

## Resources

- [String — HexDocs](https://hexdocs.pm/elixir/String.html) — read the Unicode section
- [Elixir Getting Started — Binaries, strings, and charlists](https://elixir-lang.org/getting-started/binaries-strings-and-char-lists.html)
- [Unicode in Elixir — Jose Valim's blog](https://elixir-lang.org/blog/2013/04/17/elixir-v0-8-0-released/)
- [IO.iodata_to_binary/1 — efficient string building](https://hexdocs.pm/elixir/IO.html#iodata_to_binary/1)
