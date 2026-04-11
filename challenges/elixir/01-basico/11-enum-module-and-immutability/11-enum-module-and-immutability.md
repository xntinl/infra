# Enum and Immutability: Transaction Analytics

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. The reporting system needs to compute analytics
over transaction lists: totals by category, top merchants, daily summaries.
The `Enum` module is the primary tool. This exercise focuses on WHY immutability
and eager evaluation have concrete consequences in a production system.

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
│       └── analytics.ex    # ← you implement this
├── test/
│   └── payments_cli/
│       └── analytics_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why immutability matters for analytics — not just correctness

In a mutable language, this code is dangerous:

```python
# Python — dangerous
def top_merchants(transactions):
    transactions.sort(key=lambda t: t['amount'], reverse=True)  # mutates input!
    return [t['merchant'] for t in transactions[:5]]
```

Calling `top_merchants(txs)` changes the order of `txs` for every subsequent caller.
In Elixir, `Enum.sort/2` returns a new list. The original `txs` is never touched.

This is not just a correctness guarantee — it enables safe concurrency. Multiple
processes can read the same transaction list simultaneously without locks because
no process can modify the data. This is the foundation of Elixir's concurrency model.

The practical implication for analytics: you can pass the same transaction list through
ten different `Enum` pipelines in parallel and each produces its own result without
interfering with the others.

---

## The business problem

The `Analytics` module needs to:

1. Compute revenue statistics (total, average, percentiles)
2. Find the top N merchants by transaction volume
3. Build a per-day summary grouped by date
4. Identify suspicious patterns (multiple large transactions from same merchant)

---

## Implementation

### `lib/payments_cli/analytics.ex`

```elixir
defmodule PaymentsCli.Analytics do
  @moduledoc """
  Computes analytics and reports over transaction lists.

  All functions are pure — no side effects, no mutation. The same transaction
  list can be passed to multiple functions and each produces independent results.

  Uses Enum (eager) throughout. Switch to Stream for datasets > 1M transactions
  where intermediate list allocations become a memory concern.
  """

  @doc """
  Computes revenue statistics for a list of approved transactions.

  Returns a map with :total, :count, :average, :min, :max.
  Returns {:error, :no_approved_transactions} if no approved transactions exist.

  ## Examples

      iex> txs = [
      ...>   %{status: :approved, amount_cents: 1000},
      ...>   %{status: :approved, amount_cents: 3000},
      ...>   %{status: :declined, amount_cents: 500}
      ...> ]
      iex> PaymentsCli.Analytics.revenue_stats(txs)
      {:ok, %{total: 4000, count: 2, average: 2000, min: 1000, max: 3000}}

  """
  @spec revenue_stats([map()]) :: {:ok, map()} | {:error, :no_approved_transactions}
  def revenue_stats(transactions) when is_list(transactions) do
    # TODO: implement
    #
    # Step 1: filter to approved only
    # Step 2: extract amount_cents (Enum.map)
    # Step 3: if amounts is empty, return {:error, :no_approved_transactions}
    # Step 4: compute stats
    #   total   = Enum.sum(amounts)
    #   count   = length(amounts)
    #   average = div(total, count)
    #   min     = Enum.min(amounts)
    #   max     = Enum.max(amounts)
    # Step 5: return {:ok, %{total: ..., count: ..., average: ..., min: ..., max: ...}}
  end

  @doc """
  Returns the top N merchants by total transaction amount (approved only).

  Sorted descending by total amount. Ties broken alphabetically by merchant name.

  ## Examples

      iex> txs = [
      ...>   %{status: :approved, merchant: "Shop A", amount_cents: 1000},
      ...>   %{status: :approved, merchant: "Shop B", amount_cents: 3000},
      ...>   %{status: :approved, merchant: "Shop A", amount_cents: 500}
      ...> ]
      iex> PaymentsCli.Analytics.top_merchants(txs, 2)
      [{"Shop B", 3000}, {"Shop A", 1500}]

  """
  @spec top_merchants([map()], pos_integer()) :: [{String.t(), integer()}]
  def top_merchants(transactions, n) when is_list(transactions) and is_integer(n) and n > 0 do
    # TODO: implement
    #
    # Step 1: filter to :approved
    # Step 2: Enum.group_by(fn tx -> tx.merchant end)
    #         -> %{"Shop A" => [tx1, tx2], "Shop B" => [tx3]}
    # Step 3: Enum.map each group to {merchant, total_amount}
    #         Enum.map(groups, fn {merchant, txs} -> {merchant, Enum.sum(Enum.map(txs, & &1.amount_cents))} end)
    # Step 4: Sort: Enum.sort_by(fn {_m, total} -> total end, :desc)
    # Step 5: Enum.take(n)
  end

  @doc """
  Groups transactions by date string and sums amounts per day.

  Transactions must have a :date field (string "YYYY-MM-DD").
  Returns a map %{date_string => total_cents}.

  ## Examples

      iex> txs = [
      ...>   %{date: "2024-01-15", amount_cents: 1000, status: :approved},
      ...>   %{date: "2024-01-15", amount_cents: 500,  status: :approved},
      ...>   %{date: "2024-01-16", amount_cents: 2000, status: :approved}
      ...> ]
      iex> PaymentsCli.Analytics.daily_totals(txs)
      %{"2024-01-15" => 1500, "2024-01-16" => 2000}

  """
  @spec daily_totals([map()]) :: %{String.t() => integer()}
  def daily_totals(transactions) when is_list(transactions) do
    # TODO: implement
    #
    # Step 1: filter to :approved
    # Step 2: Enum.group_by(fn tx -> tx.date end)
    # Step 3: Enum.map each group to {date, sum_of_amounts}
    # Step 4: Map.new from the list of {date, total} tuples
  end

  @doc """
  Finds merchants with suspiciously many large transactions.

  "Suspicious" means: same merchant has >= threshold_count transactions
  where each transaction amount_cents > large_amount_threshold.

  Returns a list of suspicious merchant names.

  ## Examples

      iex> txs = [
      ...>   %{merchant: "Casino", amount_cents: 50_000, status: :approved},
      ...>   %{merchant: "Casino", amount_cents: 60_000, status: :approved},
      ...>   %{merchant: "Casino", amount_cents: 55_000, status: :approved},
      ...>   %{merchant: "Coffee", amount_cents: 500,    status: :approved}
      ...> ]
      iex> PaymentsCli.Analytics.suspicious_merchants(txs, 3, 10_000)
      ["Casino"]

  """
  @spec suspicious_merchants([map()], pos_integer(), pos_integer()) :: [String.t()]
  def suspicious_merchants(transactions, threshold_count, large_amount_threshold)
      when is_list(transactions) and is_integer(threshold_count) and
             is_integer(large_amount_threshold) do
    # TODO: implement
    #
    # Step 1: filter to :approved AND amount_cents > large_amount_threshold
    # Step 2: Enum.group_by(fn tx -> tx.merchant end)
    # Step 3: filter groups where length(txs) >= threshold_count
    # Step 4: extract merchant names (map over remaining groups)
    # Step 5: sort for deterministic output
  end
end
```

### Given tests — must pass without modification

```elixir
# test/payments_cli/analytics_test.exs
defmodule PaymentsCli.AnalyticsTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Analytics

  @transactions [
    %{id: "T1", status: :approved, merchant: "Coffee Co",  amount_cents: 450,    date: "2024-01-15"},
    %{id: "T2", status: :approved, merchant: "Gas Station", amount_cents: 8000,   date: "2024-01-15"},
    %{id: "T3", status: :declined, merchant: "Coffee Co",  amount_cents: 300,    date: "2024-01-15"},
    %{id: "T4", status: :approved, merchant: "Coffee Co",  amount_cents: 520,    date: "2024-01-16"},
    %{id: "T5", status: :approved, merchant: "Gas Station", amount_cents: 7500,   date: "2024-01-16"},
    %{id: "T6", status: :approved, merchant: "Supermarket", amount_cents: 15000,  date: "2024-01-16"}
  ]

  describe "revenue_stats/1" do
    test "computes stats for approved transactions only" do
      assert {:ok, stats} = Analytics.revenue_stats(@transactions)
      # T3 is declined — excluded
      assert stats.count == 5
      # 450 + 8000 + 520 + 7500 + 15000 = 31470
      assert stats.total == 31_470
      assert stats.min == 450
      assert stats.max == 15_000
    end

    test "returns error for empty list" do
      assert {:error, :no_approved_transactions} = Analytics.revenue_stats([])
    end

    test "returns error when all transactions are declined" do
      declined = [%{status: :declined, amount_cents: 100}]
      assert {:error, :no_approved_transactions} = Analytics.revenue_stats(declined)
    end

    test "does not mutate the original list" do
      original_count = length(@transactions)
      Analytics.revenue_stats(@transactions)
      assert length(@transactions) == original_count
    end
  end

  describe "top_merchants/2" do
    test "returns top N merchants by total amount" do
      result = Analytics.top_merchants(@transactions, 2)
      assert length(result) == 2
      # Gas Station: 8000 + 7500 = 15500
      # Supermarket: 15000
      # Coffee Co: 450 + 520 = 970
      [{top_merchant, top_total} | _] = result
      # Either Gas Station or Supermarket could be first — check both candidates
      assert top_total >= 15_000
    end

    test "excludes declined transactions" do
      declined_only = [%{status: :declined, merchant: "Evil Co", amount_cents: 999_999}]
      result = Analytics.top_merchants(declined_only, 5)
      assert result == []
    end

    test "returns fewer than N when not enough merchants" do
      result = Analytics.top_merchants(@transactions, 100)
      # Only 3 unique merchants with approved transactions
      assert length(result) == 3
    end
  end

  describe "daily_totals/1" do
    test "groups and sums by date" do
      totals = Analytics.daily_totals(@transactions)
      # 2024-01-15: 450 + 8000 = 8450 (T3 declined)
      assert totals["2024-01-15"] == 8_450
      # 2024-01-16: 520 + 7500 + 15000 = 23020
      assert totals["2024-01-16"] == 23_020
    end

    test "returns empty map for empty input" do
      assert Analytics.daily_totals([]) == %{}
    end
  end

  describe "suspicious_merchants/3" do
    test "finds merchants with many large transactions" do
      txs = [
        %{merchant: "Casino", amount_cents: 50_000, status: :approved},
        %{merchant: "Casino", amount_cents: 60_000, status: :approved},
        %{merchant: "Casino", amount_cents: 55_000, status: :approved},
        %{merchant: "Coffee", amount_cents: 500,    status: :approved}
      ]
      result = Analytics.suspicious_merchants(txs, 3, 10_000)
      assert result == ["Casino"]
    end

    test "returns empty list when no merchants meet threshold" do
      result = Analytics.suspicious_merchants(@transactions, 10, 1_000)
      assert result == []
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/analytics_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Enum (eager, your impl) | Stream (lazy) | Manual recursion |
|--------|------------------------|---------------|-----------------|
| Memory | Intermediate lists allocated | No intermediate lists | Controlled by accumulator |
| When to prefer | Datasets < 1M rows, simple pipelines | Very large datasets, IO-bound | Complex custom logic |
| Debugging | `IO.inspect/2` after any step | Need to materialize first | Trace in function head |
| Parallelism | `Task.async_stream` wraps Enum | `Stream` + `Task` | Explicit |
| Code clarity | Most readable | Good for generators | Most control |

Reflection question: `revenue_stats/1` makes three passes over the approved transactions
list (filter, then map, then multiple Enum calls). Could you compute total, min, max,
and count in a single `Enum.reduce/3`? Write it. When would the single-pass version
be meaningfully faster?

---

## Common production mistakes

**1. `Enum.each/2` when you want `Enum.map/2`**
`Enum.each/2` always returns `:ok` — the transformed values are discarded.
If you compute `Enum.each(txs, fn tx -> tx.amount_cents * 2 end)`, the doubled
amounts are lost. Use `Enum.map/2` when you need the results.

**2. `Enum.sort/1` on maps sorts by key/value tuples, not by a field**
```elixir
# WRONG — sorts maps by {key, value} tuple order, not by :amount_cents
Enum.sort(transactions)

# CORRECT — sort by a specific field
Enum.sort_by(transactions, fn tx -> tx.amount_cents end, :desc)
```

**3. Chained `Enum` on very large datasets**
Five `Enum` operations on a 1M-element list creates five intermediate lists,
each 1M elements. Switch to `Stream` for lazy evaluation when intermediate
allocations become a memory concern. The change is mechanical: replace `Enum.`
with `Stream.` and add `|> Enum.to_list()` at the end.

**4. `Enum.group_by/2` and expecting sorted keys**
`Enum.group_by/2` returns a map. Maps do not guarantee key order. Always sort
the keys explicitly when the order matters for output: `Map.keys(groups) |> Enum.sort()`.

**5. Using `Enum.count/1` when pattern already filters**
```elixir
# Inefficient: builds filtered list then counts it
Enum.count(Enum.filter(txs, &(&1.status == :approved)))

# Efficient: Enum.count/2 with predicate — single pass
Enum.count(txs, fn tx -> tx.status == :approved end)
```

---

## Resources

- [Enum — HexDocs](https://hexdocs.pm/elixir/Enum.html) — especially `group_by/2`, `sort_by/3`, `reduce/3`
- [Stream — HexDocs](https://hexdocs.pm/elixir/Stream.html) — when to switch from Enum
- [Elixir School — Enum](https://elixirschool.com/en/lessons/basics/enum)
- [Elixir in Action — Chapter 4: data abstractions](https://www.manning.com/books/elixir-in-action-third-edition)
