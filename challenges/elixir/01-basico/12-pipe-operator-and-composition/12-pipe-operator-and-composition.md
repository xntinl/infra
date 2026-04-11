# Pipe Operator: Transaction Processing Pipelines

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. The reporting system chains many transformation
steps: parse, validate, enrich, filter, aggregate, format. Without `|>`, each step
becomes a nested function call. The pipe operator makes the data flow explicit and
readable — but only when used with discipline.

Project structure at this point:

```
payments_cli/
├── lib/
│   └── payments_cli/
│       ├── cli.ex
│       ├── transaction.ex
│       ├── ledger.ex
│       ├── formatter.ex
│       ├── pipeline.ex
│       ├── processor.ex
│       ├── router.ex
│       ├── analytics.ex
│       └── report.ex       # ← you implement this
├── test/
│   └── payments_cli/
│       └── report_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why `|>` is more than syntactic sugar

`data |> f(a) |> g(b)` is exactly `g(f(data, a), b)`. The semantics are identical.
The difference is cognitive: the pipe version reads in execution order, the nested
version reads inside-out.

For payment reporting, this matters because:

1. A report pipeline has 5-8 steps in a fixed order
2. Each step receives the result of the previous step
3. The data type changes through the pipeline (list -> grouped map -> sorted list -> string)

The pipe expresses the **intent** of the transformation as a sequence of steps,
not as a composition of functions. Intent-revealing code is maintainable code.

The constraint that every piped function must accept the previous result as its
**first argument** is a design pressure: it pushes you to design functions that
work well with the data they transform, not functions that take configuration first.

---

## The business problem

The `Report` module generates formatted transaction reports. Each report type is
a pipeline from raw transactions to a formatted string.

---

## Implementation

### `lib/payments_cli/report.ex`

Each public function is a pipeline: transactions in, formatted string out. Private
helpers handle the individual transformation steps. `compute_summary_stats/1` and
`format_summary/1` are given — they demonstrate how to name sub-pipelines for clarity.

The key discipline: each step transforms data, returns a value, and is independently
testable. No step has side effects. The pipe makes the data flow visible.

```elixir
defmodule PaymentsCli.Report do
  @moduledoc """
  Generates formatted transaction reports by composing transformation pipelines.

  Each public function is a pipeline: transactions in, formatted string out.
  The pipe operator makes each step explicit and independently testable.
  """

  alias PaymentsCli.Analytics

  @doc """
  Generates a summary report for a list of transactions.

  Format:
    === Transaction Summary ===
    Total transactions: N
    Approved: N (total: $X.XX)
    Declined: N
    Under review: N

  ## Examples

      iex> txs = [%{status: :approved, amount_cents: 1000, currency: "USD"}]
      iex> report = PaymentsCli.Report.summary(txs)
      iex> String.contains?(report, "Total transactions:")
      true

  """
  @spec summary([map()]) :: String.t()
  def summary(transactions) when is_list(transactions) do
    transactions
    |> compute_summary_stats()
    |> format_summary()
  end

  @doc """
  Generates a per-merchant breakdown report.

  Format: one line per merchant, sorted by total descending.
    === Merchant Report ===
    Gas Station: $155.00 total
    Supermarket: $150.00 total

  """
  @spec merchant_report([map()], pos_integer()) :: String.t()
  def merchant_report(transactions, top_n \\ 10) when is_list(transactions) do
    transactions
    |> Analytics.top_merchants(top_n)
    |> Enum.map(fn {merchant, total_cents} -> format_merchant_line(merchant, total_cents) end)
    |> then(fn lines -> ["=== Merchant Report ===" | lines] end)
    |> Enum.join("\n")
  end

  @doc """
  Generates a CSV export of approved transactions.

  Columns: id, date, merchant, amount, currency, status
  """
  @spec to_csv_report([map()]) :: String.t()
  def to_csv_report(transactions) when is_list(transactions) do
    header = "id,date,merchant,amount_cents,currency,status"

    body =
      transactions
      |> Enum.filter(fn tx -> tx.status == :approved end)
      |> Enum.sort_by(fn tx -> tx.id end)
      |> Enum.map(&transaction_to_csv_line/1)
      |> Enum.join("\n")

    header <> "\n" <> body
  end

  @doc """
  Generates a daily totals report with trend indication.

  Format:
    === Daily Report ===
    2024-01-15: $84.50
    2024-01-16: $230.20 (up 172% from prior day)

  For the first day, omit the trend indicator.
  """
  @spec daily_report([map()]) :: String.t()
  def daily_report(transactions) when is_list(transactions) do
    transactions
    |> Analytics.daily_totals()
    |> Enum.sort_by(fn {date, _} -> date end)
    |> add_trend_indicators()
    |> Enum.map(&format_daily_line/1)
    |> then(fn lines -> ["=== Daily Report ===" | lines] end)
    |> Enum.join("\n")
  end

  # ---------------------------------------------------------------------------
  # Private helpers — each one does one thing
  # ---------------------------------------------------------------------------

  @spec transaction_to_csv_line(map()) :: String.t()
  defp transaction_to_csv_line(%{id: id, amount_cents: a, currency: c, status: s} = tx) do
    date = Map.get(tx, :date, "")
    merchant = Map.get(tx, :merchant, "")
    "#{id},#{date},#{merchant},#{a},#{c},#{s}"
  end

  @spec add_trend_indicators([{String.t(), integer()}]) :: [{String.t(), integer(), String.t()}]
  defp add_trend_indicators([]), do: []

  defp add_trend_indicators([{first_date, first_total} | rest]) do
    first_entry = {first_date, first_total, ""}

    {_, result} =
      Enum.reduce(rest, {first_total, [first_entry]}, fn {date, total}, {prev, acc} ->
        trend = compute_trend(prev, total)
        {total, [{date, total, trend} | acc]}
      end)

    Enum.reverse(result)
  end

  @spec compute_trend(integer(), integer()) :: String.t()
  defp compute_trend(prev, _current) when prev == 0, do: ""

  defp compute_trend(prev, current) do
    pct = round((current - prev) / prev * 100)

    if pct >= 0 do
      "↑ #{pct}%"
    else
      "↓ #{abs(pct)}%"
    end
  end

  @spec format_daily_line({String.t(), integer(), String.t()}) :: String.t()
  defp format_daily_line({date, total_cents, ""}) do
    dollars = div(total_cents, 100)
    cents = rem(total_cents, 100)
    "#{date}: $#{dollars}.#{String.pad_leading(Integer.to_string(cents), 2, "0")}"
  end

  defp format_daily_line({date, total_cents, trend}) do
    base = format_daily_line({date, total_cents, ""})
    "#{base} (#{trend} from prior day)"
  end

  defp format_merchant_line(merchant, total_cents) do
    dollars = div(total_cents, 100)
    cents = rem(total_cents, 100)
    "#{merchant}: $#{dollars}.#{String.pad_leading(Integer.to_string(cents), 2, "0")} total"
  end

  defp compute_summary_stats(transactions) do
    approved = Enum.filter(transactions, fn tx -> tx.status == :approved end)
    declined = Enum.filter(transactions, fn tx -> tx.status == :declined end)
    flagged  = Enum.filter(transactions, fn tx -> tx.status == :flagged end)

    %{
      total: length(transactions),
      approved: length(approved),
      approved_total: approved |> Enum.map(& &1.amount_cents) |> Enum.sum(),
      declined: length(declined),
      flagged: length(flagged)
    }
  end

  defp format_summary(%{total: t, approved: a, approved_total: at, declined: d, flagged: f}) do
    dollars = div(at, 100)
    cents = rem(at, 100)
    amount_str = "$#{dollars}.#{String.pad_leading(Integer.to_string(cents), 2, "0")}"

    """
    === Transaction Summary ===
    Total transactions: #{t}
    Approved: #{a} (total: #{amount_str})
    Declined: #{d}
    Under review: #{f}
    """
    |> String.trim_trailing()
  end
end
```

**Why this works:**

- `summary/1` is a two-step pipeline: compute stats, then format. Each step is a
  named private function, making the pipeline self-documenting.

- `merchant_report/2` chains `Analytics.top_merchants/2` (which returns `[{name, total}]`),
  maps each to a formatted line, prepends the header using `then/2`, and joins with
  newlines. `then/2` is used because the transformation (prepending a header) does not
  fit the pipe pattern — the header is not an argument to a function that takes the
  list first.

- `to_csv_report/1` filters, sorts, maps to CSV lines, and joins. The header is
  concatenated separately because it is not derived from the transaction data.

- `daily_report/1` computes daily totals, sorts by date, adds trend indicators (percent
  change from prior day), formats each line, and joins. The `add_trend_indicators/1`
  helper tracks the previous day's total to compute percent change.

### Given tests — must pass without modification

```elixir
# test/payments_cli/report_test.exs
defmodule PaymentsCli.ReportTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Report

  @transactions [
    %{id: "T1", status: :approved,  merchant: "Gas Station", amount_cents: 8000,  currency: "USD", date: "2024-01-15"},
    %{id: "T2", status: :approved,  merchant: "Coffee Co",   amount_cents: 450,   currency: "USD", date: "2024-01-15"},
    %{id: "T3", status: :declined,  merchant: "Coffee Co",   amount_cents: 300,   currency: "USD", date: "2024-01-15"},
    %{id: "T4", status: :approved,  merchant: "Supermarket", amount_cents: 15000, currency: "USD", date: "2024-01-16"},
    %{id: "T5", status: :flagged,   merchant: "Casino",      amount_cents: 50000, currency: "USD", date: "2024-01-16"}
  ]

  describe "summary/1" do
    test "includes total count" do
      result = Report.summary(@transactions)
      assert String.contains?(result, "Total transactions: 5")
    end

    test "includes approved count" do
      result = Report.summary(@transactions)
      assert String.contains?(result, "Approved: 3")
    end

    test "includes declined count" do
      result = Report.summary(@transactions)
      assert String.contains?(result, "Declined: 1")
    end

    test "returns a string" do
      assert is_binary(Report.summary(@transactions))
    end
  end

  describe "merchant_report/2" do
    test "includes report header" do
      result = Report.merchant_report(@transactions)
      assert String.contains?(result, "=== Merchant Report ===")
    end

    test "includes merchant names" do
      result = Report.merchant_report(@transactions)
      assert String.contains?(result, "Gas Station")
      assert String.contains?(result, "Supermarket")
    end

    test "excludes declined and flagged transactions from totals" do
      result = Report.merchant_report(@transactions)
      # Coffee Co only has 1 approved transaction (T2, $4.50)
      # The declined T3 should not appear in the total
      assert String.contains?(result, "Coffee Co")
    end
  end

  describe "to_csv_report/1" do
    test "includes CSV header" do
      result = Report.to_csv_report(@transactions)
      assert String.starts_with?(result, "id,date,merchant,amount_cents,currency,status")
    end

    test "includes approved transactions only" do
      result = Report.to_csv_report(@transactions)
      assert String.contains?(result, "T1")
      assert String.contains?(result, "T2")
      assert String.contains?(result, "T4")
      # T3 is declined, T5 is flagged
      refute String.contains?(result, "T3")
      refute String.contains?(result, "T5")
    end
  end

  describe "daily_report/1" do
    test "includes daily report header" do
      result = Report.daily_report(@transactions)
      assert String.contains?(result, "=== Daily Report ===")
    end

    test "includes both dates" do
      result = Report.daily_report(@transactions)
      assert String.contains?(result, "2024-01-15")
      assert String.contains?(result, "2024-01-16")
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/report_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Pipe chains (your impl) | Nested function calls | Intermediate variables |
|--------|------------------------|-----------------------|----------------------|
| Readability | Execution order matches code order | Read inside-out | Clear names, more lines |
| Debuggability | Insert `IO.inspect` at any step | Must break apart | Inspect any variable |
| Refactoring | Add/remove steps trivially | Rewrite nesting | Update both sides |
| Performance | No difference at runtime | No difference | No difference |
| When to prefer | Transformations with clear data flow | Single expression needed | When intermediate values need names |

Reflection question: `merchant_report/1` uses `then/2` to prepend the header.
What does `then/2` do, and why is it useful in a pipeline? When would you prefer
a different approach (like computing the header outside the pipe)?

---

## Common production mistakes

**1. Piping to functions where the data is not the first argument**
`transactions |> Enum.member?(target)` works because `member?` takes the
enumerable first. But `transactions |> String.contains?("search")` does not
work — `String.contains?` expects the string first, then the pattern.
Use a wrapper lambda: `|> then(fn txs -> String.contains?(format(txs), "search") end)`.

**2. Pipes without parentheses fail**
```elixir
# WRONG — length without () is not a function call in pipe position
[1, 2, 3] |> length
# ** (CompileError) undefined function length/0

# CORRECT
[1, 2, 3] |> length()
```
Always use parentheses on the right side of `|>`.

**3. Overly long pipelines lose context**
A pipeline with 15 steps is as hard to read as deeply nested calls. When a pipeline
grows beyond 6-8 steps, extract named private functions for sub-pipelines.
`compute_summary_stats/1` and `format_summary/1` are good examples — they give
names to what would otherwise be anonymous sections of a long pipeline.

**4. `IO.inspect/2` left in production code**
Inserting `|> IO.inspect(label: "debug")` into a pipeline is the correct debugging
technique. But `IO.inspect` has a return value (it returns its argument), so it
is invisible to the pipeline. This makes it easy to forget to remove before merging.
Enable `--warnings-as-errors` and treat `IO.inspect` calls as lint warnings in CI.

**5. Pipelines that change the data type unexpectedly**
A pipeline that starts with `[map()]` and ends with a `String.t()` is correct —
but if an intermediate step returns `{:ok, list}` instead of `list`, the next
step receives a tuple instead of a list and crashes. Make the type flow explicit
through `@spec` annotations.

---

## Resources

- [Pipe operator — Kernel docs](https://hexdocs.pm/elixir/Kernel.html#%7C%3E/2)
- [then/2 — Kernel docs](https://hexdocs.pm/elixir/Kernel.html#then/2)
- [IO.inspect/2 — HexDocs](https://hexdocs.pm/elixir/IO.html#inspect/2)
- [Elixir School — Pipe Operator](https://elixirschool.com/en/lessons/basics/pipe_operator)
