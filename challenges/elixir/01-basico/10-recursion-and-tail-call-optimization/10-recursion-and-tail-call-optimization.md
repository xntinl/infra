# Recursion and TCO: Building the Transaction Report

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. The `Ledger` module needs functions that walk a
list of transactions to compute reports. This exercise explores recursion as the
fundamental loop mechanism in Elixir, and why tail-call optimization (TCO) determines
whether your code handles 1,000 transactions or 1,000,000.

Project structure at this point:

```
payments_cli/
├── lib/
│   └── payments_cli/
│       ├── cli.ex
│       ├── transaction.ex
│       ├── ledger.ex           # ← you extend this
│       ├── formatter.ex
│       ├── pipeline.ex
│       ├── processor.ex
│       └── router.ex
├── test/
│   └── payments_cli/
│       └── ledger_recursion_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why TCO matters in a payments context

A bank transaction export can have millions of rows. A function that processes
each transaction naively — building up a stack frame per transaction — will exhaust
the process stack before finishing.

The BEAM VM provides a guarantee: **a tail call never grows the stack**. The current
frame is reused. This is not an optimization; it is a language contract. Erlang and
Elixir GenServers are implemented as infinite recursive loops that never blow the
stack precisely because of this guarantee.

The key distinction:

```elixir
# NOT a tail call — the + happens AFTER the recursive call returns
def sum([h | t]), do: h + sum(t)
#                         ^^^^^^ this result is needed to compute + h
# Stack grows: sum([1,2,3]) needs sum([2,3]) needs sum([3]) needs sum([])

# Tail call — recursive call IS the last operation
def sum([h | t], acc), do: sum(t, h + acc)
#                          ^^^^^^^^^^^^^^^ this IS the last thing that happens
# Stack stays flat: each call replaces the current frame
```

The accumulator pattern converts a naive recursive function to a tail-recursive one
by moving the "work in progress" into an argument.

---

## The business problem

Extend the `Ledger` module with report-building functions that must handle large
transaction lists without stack overflow:

1. Count transactions by status (tail-recursive counter)
2. Find all transactions above an amount threshold (tail-recursive filter)
3. Compute a CSV report string from a transaction list (tail-recursive string builder)
4. Verify that both naive and TCO versions produce identical results

---

## Implementation

### Extend `lib/payments_cli/ledger.ex`

Each function uses the accumulator pattern for tail recursion. The public function
accepts the user-facing arguments and delegates to a private helper that adds the
accumulator. The private helper has three clauses: base case (empty list), matching
case (element qualifies), and non-matching case (skip and continue).

```elixir
# Add to PaymentsCli.Ledger

@doc """
Counts the number of transactions matching the given status.

Implemented with TCO — safe for arbitrarily long transaction lists.

## Examples

    iex> txs = [%{status: :approved}, %{status: :declined}, %{status: :approved}]
    iex> PaymentsCli.Ledger.count_by_status(txs, :approved)
    2

"""
@spec count_by_status([map()], atom()) :: non_neg_integer()
def count_by_status(transactions, status) when is_list(transactions) and is_atom(status) do
  count_by_status(transactions, status, 0)
end

defp count_by_status([], _status, acc), do: acc

defp count_by_status([%{status: s} | rest], status, acc) when s == status do
  count_by_status(rest, status, acc + 1)
end

defp count_by_status([_ | rest], status, acc) do
  count_by_status(rest, status, acc)
end

@doc """
Returns all transactions where amount_cents exceeds the threshold.

Tail-recursive. Preserves order of the original list.

## Examples

    iex> txs = [%{id: "A", amount_cents: 100}, %{id: "B", amount_cents: 500}, %{id: "C", amount_cents: 50}]
    iex> PaymentsCli.Ledger.above_threshold(txs, 200)
    [%{id: "B", amount_cents: 500}]

"""
@spec above_threshold([map()], integer()) :: [map()]
def above_threshold(transactions, threshold_cents)
    when is_list(transactions) and is_integer(threshold_cents) do
  do_above_threshold(transactions, threshold_cents, [])
end

defp do_above_threshold([], _threshold, acc), do: Enum.reverse(acc)

defp do_above_threshold([%{amount_cents: amount} = tx | rest], threshold, acc)
     when amount > threshold do
  do_above_threshold(rest, threshold, [tx | acc])
end

defp do_above_threshold([_ | rest], threshold, acc) do
  do_above_threshold(rest, threshold, acc)
end

@doc """
Builds a CSV report string from a list of transactions.

Format: one line per transaction, comma-separated, with header row.
"id,amount_cents,currency,status\n" + one line per transaction.

Tail-recursive — builds the line list with prepend, then joins at the end.

## Examples

    iex> txs = [%{id: "T1", amount_cents: 1000, currency: "USD", status: :approved}]
    iex> report = PaymentsCli.Ledger.to_csv(txs)
    iex> String.starts_with?(report, "id,amount_cents,currency,status")
    true
    iex> String.contains?(report, "T1,1000,USD,approved")
    true

"""
@spec to_csv([map()]) :: String.t()
def to_csv(transactions) when is_list(transactions) do
  header = "id,amount_cents,currency,status"
  lines = build_csv_lines(transactions, [])
  all_lines = [header | Enum.reverse(lines)]
  Enum.join(all_lines, "\n")
end

defp build_csv_lines([], acc), do: acc

defp build_csv_lines([tx | rest], acc) do
  line = "#{tx.id},#{tx.amount_cents},#{tx.currency},#{tx.status}"
  build_csv_lines(rest, [line | acc])
end
```

**Why this works:**

- `count_by_status/2` delegates to `count_by_status/3` with an initial accumulator of `0`.
  The three private clauses handle: empty list (return accumulator), matching status
  (increment and recurse), non-matching status (skip and recurse). Every recursive call
  is in tail position — it is the last operation in the function body.

- `above_threshold/2` delegates to `do_above_threshold/3` with an empty accumulator.
  Matching transactions are prepended to the accumulator (O(1)). The base case reverses
  the accumulator once (O(n)) to restore the original order. Total: O(n).

- `to_csv/1` delegates to `build_csv_lines/2` which builds lines in reverse order by
  prepending each line to the accumulator. After the recursion, `Enum.reverse/1` restores
  the correct order, then `Enum.join/2` combines header and lines with newlines.

- All three functions are safe for 100k+ elements because every recursive call is a
  tail call — the BEAM reuses the current stack frame.

### Given tests — must pass without modification

```elixir
# test/payments_cli/ledger_recursion_test.exs
defmodule PaymentsCli.LedgerRecursionTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Ledger

  # Generate a large list to verify TCO (no stack overflow)
  @large_count 100_000
  @large_txs Enum.map(1..@large_count, fn i ->
    status = if rem(i, 3) == 0, do: :declined, else: :approved
    %{id: "T#{i}", amount_cents: i * 10, currency: "USD", status: status}
  end)

  describe "count_by_status/2" do
    test "counts approved transactions" do
      txs = [
        %{status: :approved},
        %{status: :declined},
        %{status: :approved},
        %{status: :flagged}
      ]
      assert Ledger.count_by_status(txs, :approved) == 2
    end

    test "returns 0 when no match" do
      assert Ledger.count_by_status([%{status: :approved}], :declined) == 0
    end

    test "handles empty list" do
      assert Ledger.count_by_status([], :approved) == 0
    end

    test "handles 100k transactions without stack overflow" do
      # If not tail-recursive, this crashes with stack overflow
      result = Ledger.count_by_status(@large_txs, :approved)
      # 2/3 of 100k are approved (those not divisible by 3)
      assert result > 0
      assert result < @large_count
    end
  end

  describe "above_threshold/2" do
    test "filters by threshold" do
      txs = [
        %{id: "A", amount_cents: 100},
        %{id: "B", amount_cents: 500},
        %{id: "C", amount_cents: 50}
      ]
      result = Ledger.above_threshold(txs, 200)
      assert length(result) == 1
      assert hd(result).id == "B"
    end

    test "preserves original order" do
      txs = [
        %{id: "Z", amount_cents: 1000},
        %{id: "A", amount_cents: 900}
      ]
      [first, second] = Ledger.above_threshold(txs, 500)
      assert first.id == "Z"
      assert second.id == "A"
    end

    test "returns empty for all below threshold" do
      txs = [%{id: "X", amount_cents: 10}]
      assert Ledger.above_threshold(txs, 100) == []
    end

    test "handles 100k transactions without stack overflow" do
      result = Ledger.above_threshold(@large_txs, 500_000)
      assert is_list(result)
    end
  end

  describe "to_csv/1" do
    test "includes header row" do
      result = Ledger.to_csv([])
      assert String.starts_with?(result, "id,amount_cents,currency,status")
    end

    test "includes transaction data" do
      txs = [%{id: "T1", amount_cents: 1000, currency: "USD", status: :approved}]
      result = Ledger.to_csv(txs)
      assert String.contains?(result, "T1,1000,USD,approved")
    end

    test "handles 100k transactions without stack overflow" do
      result = Ledger.to_csv(@large_txs)
      line_count = result |> String.split("\n") |> length()
      # header + 100k lines
      assert line_count == @large_count + 1
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/ledger_recursion_test.exs --trace
```

The 100k transaction tests will fail if your implementation is not tail-recursive —
they exist specifically to surface stack overflow.

---

## Trade-off analysis

| Aspect | Tail-recursive with accumulator | Naive recursion | `Enum.reduce/3` |
|--------|--------------------------------|-----------------|-----------------|
| Stack usage | O(1) — constant | O(n) — grows with list | O(1) — implemented with TCO internally |
| Max list size | Unlimited | ~10k-100k depending on frame size | Unlimited |
| Code clarity | Requires accumulator pattern knowledge | Reads like math | Most readable |
| When to use | When Enum doesn't fit the logic | Learning, or guaranteed small lists | Production code |
| Performance | Comparable to Enum | Comparable for small n | Preferred |

Reflection question: `to_csv/1` builds lines with prepend + reverse. If the CSV
report is later streamed to a file instead of held in memory, how would the
implementation change? Think about `Stream.map/2` and `IO.stream/2`.

---

## Common production mistakes

**1. Naive recursion on user-provided data**
A bank file with 1M transactions crashes a naive recursive sum. The call stack
default is ~10,000 frames on most BEAM configurations. Use TCO or `Enum` for
anything that processes external data.

**2. Accumulator in wrong argument position**
The tail call must be the **last** operation. `n * factorial(n-1, acc)` is NOT a
tail call — the multiplication happens after the recursive call returns. The tail
call form is `factorial(n-1, n * acc)` — the multiplication is in the argument,
not in the return expression.

**3. Forgetting `Enum.reverse/1` after prepend-accumulation**
The accumulator reverses the order because `[new_item | acc]` prepends. Without
`Enum.reverse/1` at the end, your results are backwards. This produces subtle
bugs in ordered reports that are hard to catch without a large test dataset.

**4. Using `++` in the accumulator**
`acc ++ [new_item]` in every recursive step is O(n²). Use `[new_item | acc]` then
reverse once. With 100k transactions, the difference is milliseconds vs minutes.

**5. Reinventing `Enum` in production code**
The exercises build recursive functions to teach the mechanism. In production, use
`Enum.count/2`, `Enum.filter/2`, `Enum.map_join/3` — they are correct, optimized,
and readable. Write manual recursion only when the logic genuinely does not fit
the Enum API (tree traversal, state machines, non-linear accumulation).

---

## Resources

- [Recursion — Elixir Getting Started](https://elixir-lang.org/getting-started/recursion.html)
- [Erlang Efficiency Guide — Tail Recursion](https://www.erlang.org/doc/efficiency_guide/eff_guide.html)
- [Enum — HexDocs](https://hexdocs.pm/elixir/Enum.html)
- [Elixir in Action — Saša Jurić — Chapter 3 (recursion and loops)](https://www.manning.com/books/elixir-in-action-third-edition)
