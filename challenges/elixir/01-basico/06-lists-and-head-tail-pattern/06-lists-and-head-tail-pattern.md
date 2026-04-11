# Lists and Head/Tail: Processing Transaction Batches

**Project**: `payments_cli` — a CLI tool that processes payment transactions

---

## Project context

You are building `payments_cli`, a CLI tool that processes payment transactions from CSV
files, validates them, applies business rules, and produces ledger reports.

This exercise implements list-processing functions in a `Ledger` module: filtering
transactions by status, finding the transaction with the highest amount, grouping
transactions by currency, and computing running balances. These operations require
understanding lists as linked structures and knowing when O(1) prepend vs O(n)
append changes everything at scale.

---

## Why linked lists matter at the implementation level

A list in Elixir is not an array. `[1, 2, 3]` is syntactic sugar for:

```elixir
[1 | [2 | [3 | []]]]
```

Each cons cell holds a value (head) and a pointer to the tail. This structure
has concrete performance consequences for payments processing:

- **`[tx | accumulator]`** — O(1). Creates one cons cell. Use this inside
  `Enum.reduce` to build result lists.
- **`accumulator ++ [tx]`** — O(n). Traverses the entire `accumulator` to append.
  With 100,000 transactions, this turns O(n) processing into O(n^2).
- **`Enum.reverse/1` at the end** — O(n) once. Add this after the reduce to
  restore order. Total cost: O(n). Contrast with repeated append: O(n^2).

The canonical pattern for building lists in Elixir:

```elixir
# O(n) — correct
list
|> Enum.reduce([], fn x, acc -> [transform(x) | acc] end)
|> Enum.reverse()
```

In production code you use `Enum.map/2` which does this internally. But when you
write a custom `Enum.reduce/3` that builds a list, you must apply this pattern.

---

## The business problem

The `Ledger` module needs list-processing functions:

1. Filter transactions by status
2. Find the transaction with the highest amount
3. Group transactions by currency
4. Build a running balance list from a sequence of transactions

---

## Implementation

### `lib/payments_cli/ledger.ex`

Each function uses the appropriate `Enum` function for the task. `filter_by_status/2`
delegates to `Enum.filter/2`. `max_transaction/1` uses `Enum.reduce/3` with the first
element as the initial accumulator — this avoids the "what is the initial max?" problem.
`group_by_currency/1` delegates to `Enum.group_by/2`, which preserves order within
each group. `running_balance/1` uses `Enum.reduce/3` with a tuple accumulator to
track both the running total and the result list.

```elixir
defmodule PaymentsCli.Ledger do
  @moduledoc """
  List-processing functions for the payments ledger.

  Provides filtering, grouping, aggregation, and running balance
  computations over transaction lists. All functions are pure — they
  return new lists without modifying the input.
  """

  @doc """
  Filters transactions to those matching the given status atom.

  Returns a list of matching transaction maps, preserving order.

  ## Examples

      iex> txs = [%{status: :approved, amount_cents: 100}, %{status: :declined, amount_cents: 50}]
      iex> PaymentsCli.Ledger.filter_by_status(txs, :approved)
      [%{status: :approved, amount_cents: 100}]

  """
  @spec filter_by_status([map()], atom()) :: [map()]
  def filter_by_status(transactions, status) when is_list(transactions) and is_atom(status) do
    Enum.filter(transactions, fn tx -> tx.status == status end)
  end

  @doc """
  Returns the transaction with the highest amount_cents.

  Returns {:ok, transaction} or {:error, :empty_list} for an empty list.

  ## Examples

      iex> txs = [%{id: "A", amount_cents: 500}, %{id: "B", amount_cents: 1200}, %{id: "C", amount_cents: 300}]
      iex> PaymentsCli.Ledger.max_transaction(txs)
      {:ok, %{id: "B", amount_cents: 1200}}

  """
  @spec max_transaction([map()]) :: {:ok, map()} | {:error, :empty_list}
  def max_transaction([]), do: {:error, :empty_list}

  def max_transaction([first | rest]) do
    winner =
      Enum.reduce(rest, first, fn tx, current_max ->
        if tx.amount_cents > current_max.amount_cents, do: tx, else: current_max
      end)

    {:ok, winner}
  end

  @doc """
  Groups transactions by currency into a map of %{currency => [transactions]}.

  Enum.group_by/2 preserves the order of elements within each group.

  ## Examples

      iex> txs = [
      ...>   %{currency: "USD", amount_cents: 100},
      ...>   %{currency: "EUR", amount_cents: 200},
      ...>   %{currency: "USD", amount_cents: 150}
      ...> ]
      iex> PaymentsCli.Ledger.group_by_currency(txs)
      %{"EUR" => [%{currency: "EUR", amount_cents: 200}], "USD" => [%{currency: "USD", amount_cents: 100}, %{currency: "USD", amount_cents: 150}]}

  """
  @spec group_by_currency([map()]) :: %{String.t() => [map()]}
  def group_by_currency(transactions) when is_list(transactions) do
    Enum.group_by(transactions, fn tx -> tx.currency end)
  end

  @doc """
  Builds a running balance list from a list of approved transaction amounts.

  Each element is the cumulative sum up to and including that transaction.

  ## Examples

      iex> PaymentsCli.Ledger.running_balance([100, 200, 50])
      [100, 300, 350]

      iex> PaymentsCli.Ledger.running_balance([])
      []

  """
  @spec running_balance([integer()]) :: [integer()]
  def running_balance(amounts) when is_list(amounts) do
    amounts
    |> Enum.reduce({0, []}, fn amount, {total, acc} ->
      new_total = total + amount
      {new_total, [new_total | acc]}
    end)
    |> then(fn {_total, list} -> Enum.reverse(list) end)
  end
end
```

**Why this works:**

- `filter_by_status/2` delegates to `Enum.filter/2` with a predicate that compares
  `tx.status` to the target atom. Atom comparison is O(1). The function preserves
  order because `Enum.filter/2` iterates left-to-right.

- `max_transaction/1` handles the empty list explicitly (returns `{:error, :empty_list}`),
  then uses `Enum.reduce/3` with `first` as the initial accumulator. This avoids needing
  a sentinel value like `0` or `-infinity` — the initial max is always a real transaction.

- `group_by_currency/1` delegates to `Enum.group_by/2`, which internally uses
  `Map.update/4` to build the groups. Order within each group is preserved (elements
  appear in the same order as in the original list). The map keys have no guaranteed
  order — do not rely on `Map.keys/1` being sorted.

- `running_balance/1` uses a tuple accumulator `{running_total, result_list}` in
  `Enum.reduce/3`. Each step adds the current amount to the running total and prepends
  the new total to the result list (O(1)). After the reduce, `Enum.reverse/1` restores
  the original order (O(n) once). The `then/2` function unwraps the tuple.

### Tests

```elixir
# test/payments_cli/ledger_list_test.exs
defmodule PaymentsCli.LedgerListTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Ledger

  @transactions [
    %{id: "T1", status: :approved, currency: "USD", amount_cents: 1000},
    %{id: "T2", status: :declined, currency: "EUR", amount_cents: 500},
    %{id: "T3", status: :approved, currency: "USD", amount_cents: 2500},
    %{id: "T4", status: :flagged,  currency: "EUR", amount_cents: 750},
    %{id: "T5", status: :approved, currency: "GBP", amount_cents: 300}
  ]

  describe "filter_by_status/2" do
    test "filters approved transactions" do
      result = Ledger.filter_by_status(@transactions, :approved)
      assert length(result) == 3
      assert Enum.all?(result, fn tx -> tx.status == :approved end)
    end

    test "returns empty list when no match" do
      result = Ledger.filter_by_status(@transactions, :reversed)
      assert result == []
    end

    test "empty input returns empty output" do
      assert Ledger.filter_by_status([], :approved) == []
    end
  end

  describe "max_transaction/1" do
    test "finds the transaction with highest amount" do
      assert {:ok, tx} = Ledger.max_transaction(@transactions)
      assert tx.id == "T3"
      assert tx.amount_cents == 2500
    end

    test "returns error for empty list" do
      assert {:error, :empty_list} = Ledger.max_transaction([])
    end

    test "works with a single transaction" do
      single = [%{id: "X", amount_cents: 999}]
      assert {:ok, %{id: "X"}} = Ledger.max_transaction(single)
    end
  end

  describe "group_by_currency/1" do
    test "groups transactions by currency" do
      groups = Ledger.group_by_currency(@transactions)
      assert map_size(groups) == 3
      assert length(groups["USD"]) == 2
      assert length(groups["EUR"]) == 2
      assert length(groups["GBP"]) == 1
    end

    test "empty input returns empty map" do
      assert Ledger.group_by_currency([]) == %{}
    end
  end

  describe "running_balance/1" do
    test "computes running balance" do
      assert Ledger.running_balance([100, 200, 50]) == [100, 300, 350]
    end

    test "single element" do
      assert Ledger.running_balance([500]) == [500]
    end

    test "empty list" do
      assert Ledger.running_balance([]) == []
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/ledger_list_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Prepend + reverse (your impl) | `++` append per step | `Enum.map` / `Enum.filter` |
|--------|-------------------------------|---------------------|--------------------------|
| Time complexity | O(n) | O(n^2) | O(n) — preferred for simple transforms |
| Memory per step | One cons cell | Copies entire list | One cons cell |
| Code clarity | Requires knowing the pattern | Looks natural but slow | Most readable |
| When to use | Custom accumulation logic | Never in reduce | Standard transforms |

Reflection question: `Enum.group_by/2` internally uses `Map.update/4` to build the
groups. What does its implementation look like if you had to write it from scratch
using `Enum.reduce/3`? Write the equivalent manually as a mental exercise.

---

## Common production mistakes

**1. `++` in reduce builds O(n^2) pipelines**
The most common performance bug in new Elixir code. With 100,000 transactions,
`accumulator ++ [tx]` turns a 100ms operation into minutes. Profile with
`:timer.tc/1` before and after fixing.

**2. Calling `hd/1` or `tl/1` on potentially empty lists**
`hd([])` raises `ArgumentError`. In transaction processing, an empty batch is
a valid input. Pattern match with `case`:
```elixir
case transactions do
  [] -> handle_empty()
  [first | rest] -> process(first, rest)
end
```

**3. Using `Enum.at/2` in a loop**
`Enum.at(list, n)` traverses the list to index `n` — it is O(n). Calling it
inside a loop over the list is O(n^2). Use pattern matching or `Enum.with_index/2`
instead.

**4. List `--` removes only the first occurrence**
`[1, 2, 2, 3] -- [2]` returns `[1, 2, 3]`, not `[1, 3]`. Use `Enum.reject/2`
to remove all occurrences: `Enum.reject(list, &(&1 == 2))`.

**5. Assuming list ordering from `Enum.group_by/2`**
`Enum.group_by/2` preserves insertion order within each group. But the map
itself does not guarantee key ordering. Never iterate map keys and assume
alphabetical or insertion order without an explicit sort.

---

## Resources

- [List — HexDocs](https://hexdocs.pm/elixir/List.html)
- [Enum — HexDocs](https://hexdocs.pm/elixir/Enum.html) — `group_by/2`, `reduce/3`, `max_by/2`
- [Erlang efficiency guide — list handling](https://www.erlang.org/doc/efficiency_guide/eff_guide.html)
- [Elixir Getting Started — Lists](https://elixir-lang.org/getting-started/basic-types.html#lists-or-tuples)
