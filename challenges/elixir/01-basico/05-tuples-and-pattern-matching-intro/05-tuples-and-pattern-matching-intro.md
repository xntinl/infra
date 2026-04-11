# Tuples and Pattern Matching: Transaction Result Handling

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. Every operation in the payments pipeline — parsing,
validation, processing — can fail. Elixir's pattern matching and the `{:ok, value}` /
`{:error, reason}` convention are the mechanism for expressing and handling these
failures explicitly.

Project structure at this point:

```
payments_cli/
├── lib/
│   └── payments_cli/
│       ├── cli.ex
│       ├── transaction.ex
│       ├── ledger.ex
│       ├── formatter.ex
│       └── pipeline.ex     # ← you implement this
├── test/
│   └── payments_cli/
│       └── pipeline_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why `=` is not assignment in Elixir

In Python and Java, `x = 5` assigns the value `5` to `x`. In Elixir, `x = 5` is
a **match expression**. The left side must be compatible with the right side, or
the process raises `MatchError`.

This distinction becomes important when you write:

```elixir
{:ok, transaction} = process(line)
```

This is not "assign the result to a variable called `transaction`". It is:
"assert that `process/1` returned a two-element tuple where the first element
is the atom `:ok`, and bind the second element to the variable `transaction`".
If `process/1` returns `{:error, :invalid_amount}`, the match fails and the
process dies with a `MatchError`.

That failure is **not a bug** — it is Elixir's fail-fast design. A MatchError
with a clear message (`no match of right hand side value: {:error, :invalid_amount}`)
is better than silently continuing with corrupted data.

---

## The business problem

The `Pipeline` module needs to:

1. Process a single CSV line through parse → validate → classify
2. Return `{:ok, transaction_map}` on success
3. Return `{:error, reason}` on any failure, identifying which step failed
4. Process a batch of lines and separate successes from failures

---

## Implementation

### `lib/payments_cli/pipeline.ex`

```elixir
defmodule PaymentsCli.Pipeline do
  @moduledoc """
  Orchestrates the transaction processing pipeline.

  Each step returns {:ok, data} or {:error, reason}. The pipeline
  stops at the first failure and propagates the error upward.
  This is the explicit error handling model — no exceptions, no nil checks.
  """

  alias PaymentsCli.{Formatter, Transaction}

  @doc """
  Processes a single CSV line through the full pipeline.

  Returns {:ok, transaction} with a validated transaction map,
  or {:error, {step, reason}} identifying which step failed.

  ## Examples

      iex> PaymentsCli.Pipeline.process_line("TXN001,1234,USD,Coffee Shop,approved")
      {:ok, %{id: "TXN001", amount_cents: 1234, currency: "USD", merchant: "Coffee Shop", status: :approved}}

      iex> PaymentsCli.Pipeline.process_line("bad data")
      {:error, {:parse, "expected 5 fields, got 1"}}

  """
  @spec process_line(String.t()) :: {:ok, map()} | {:error, {atom(), term()}}
  def process_line(line) when is_binary(line) do
    # TODO: implement the pipeline
    #
    # Step 1: parse with Formatter.parse_csv_line/1
    #   On {:error, reason}, return {:error, {:parse, reason}}
    #
    # Step 2: validate the parsed map (check amount_cents > 0, currency is 3 chars)
    #   On failure, return {:error, {:validate, reason}}
    #
    # Step 3: convert string status to atom with Transaction.parse_status/1
    #   On {:error, reason}, return {:error, {:status, reason}}
    #
    # Return {:ok, map_with_atom_status} on success
    #
    # HINT: use case expressions to pattern match on each step result.
    # In exercise 09 you'll learn `with` which makes this more concise —
    # for now, use nested case expressions.
  end

  @doc """
  Processes a list of CSV lines and separates results.

  Returns {successful_transactions, errors} where:
  - successful_transactions is a list of transaction maps
  - errors is a list of {line_number, error} tuples

  ## Examples

      iex> lines = ["TXN001,1000,USD,Shop,approved", "bad", "TXN002,500,USD,Cafe,pending"]
      iex> {ok, errors} = PaymentsCli.Pipeline.process_batch(lines)
      iex> length(ok)
      2
      iex> length(errors)
      1

  """
  @spec process_batch([String.t()]) :: {[map()], [{pos_integer(), term()}]}
  def process_batch(lines) when is_list(lines) do
    # TODO: implement batch processing
    #
    # HINT: use Enum.with_index(lines, 1) to get {line, line_number} pairs.
    # Then Enum.reduce to build two accumulators: one for successes, one for errors.
    # Pattern match on {:ok, tx} and {:error, reason} from process_line/1.
    #
    # Remember: prepend to accumulators (O(1)) then Enum.reverse at the end.
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  @spec validate_transaction(map()) :: {:ok, map()} | {:error, String.t()}
  defp validate_transaction(%{amount_cents: amount, currency: currency} = tx) do
    # TODO: validate amount_cents > 0 and String.length(currency) == 3
    # Return {:ok, tx} on success, {:error, reason_string} on failure
  end
end
```

### Given tests — must pass without modification

```elixir
# test/payments_cli/pipeline_test.exs
defmodule PaymentsCli.PipelineTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Pipeline

  describe "process_line/1" do
    test "processes a valid line" do
      assert {:ok, tx} = Pipeline.process_line("TXN001,1234,USD,Coffee Shop,approved")
      assert tx.id == "TXN001"
      assert tx.amount_cents == 1234
      assert tx.status == :approved
    end

    test "returns parse error for bad format" do
      assert {:error, {:parse, _reason}} = Pipeline.process_line("not csv at all")
    end

    test "returns validate error for negative amount" do
      assert {:error, {:validate, _reason}} = Pipeline.process_line("TXN001,-100,USD,Shop,approved")
    end

    test "returns validate error for zero amount" do
      assert {:error, {:validate, _reason}} = Pipeline.process_line("TXN001,0,USD,Shop,approved")
    end

    test "returns status error for unknown status" do
      assert {:error, {:status, _reason}} = Pipeline.process_line("TXN001,100,USD,Shop,exploded")
    end

    test "converts status string to atom" do
      assert {:ok, tx} = Pipeline.process_line("TXN001,500,USD,Shop,pending")
      assert is_atom(tx.status)
      assert tx.status == :pending
    end
  end

  describe "process_batch/1" do
    test "separates successes from errors" do
      lines = [
        "TXN001,1000,USD,Shop A,approved",
        "bad line",
        "TXN002,500,USD,Shop B,pending"
      ]

      {successes, errors} = Pipeline.process_batch(lines)
      assert length(successes) == 2
      assert length(errors) == 1
    end

    test "errors include line number" do
      lines = ["good,100,USD,Shop,approved", "bad"]
      {_ok, [{line_number, _reason}]} = Pipeline.process_batch(lines)
      assert line_number == 2
    end

    test "empty batch returns empty results" do
      assert {[], []} = Pipeline.process_batch([])
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/pipeline_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `{:ok, v}` / `{:error, r}` (your impl) | Raise exceptions | `nil` returns |
|--------|----------------------------------------|-----------------|---------------|
| Error visibility | Forced — caller must handle | Implicit — may be uncaught | Silent — nil propagates |
| Error context | Tagged with step: `{:parse, reason}` | Stack trace | None |
| Pattern exhaustiveness | Dialyzer can check | Nothing enforced | Nothing enforced |
| Code structure | `case` / `with` chains | `try/rescue` | `if x != nil` chains |
| Testing | Test each branch explicitly | Rescue in tests | Check for nil |

Reflection question: `process_line/1` uses nested `case` expressions. In exercise 09
you will learn the `with` macro. Compare the two approaches — what does `with` add
beyond syntactic convenience?

---

## Common production mistakes

**1. Bare match `{:ok, x} = function()` in non-trivial code**
A bare match crashes the process on failure. This is intentional in tests
(`assert {:ok, x} = ...`) and in contexts where failure should propagate to
a supervisor. In a pipeline where you want to collect errors, use `case`.

**2. Deeply nested `case` expressions**
Three levels of `case` for three pipeline steps is the warning sign that you
need `with`. `with` flattens the happy path and makes error handling at the bottom.

**3. Losing error context through re-wrapping**
```elixir
case step1() do
  {:error, _} -> {:error, "step1 failed"}  # BAD: lost the original reason
  {:error, reason} -> {:error, {:step1, reason}}  # GOOD: reason preserved
end
```
Always preserve the original error reason when re-wrapping.

**4. The pin operator `^` is often needed in tests**
```elixir
expected_id = "TXN001"
assert {:ok, %{id: ^expected_id}} = Pipeline.process_line(line)
```
Without `^`, `id: expected_id` would rebind `expected_id` to whatever the map
contains, and the assertion would always pass.

**5. Ignoring the wildcard `_` warning**
The compiler warns when a match arm uses `_` in a position that hides data.
A test with `{:error, _}` silently passes even if the error reason changes.
Use `{:error, message}` and assert on `message` when the reason matters.

---

## Resources

- [Pattern Matching — Elixir Getting Started](https://elixir-lang.org/getting-started/pattern-matching.html)
- [Tuple — HexDocs](https://hexdocs.pm/elixir/Tuple.html)
- [with — Kernel.SpecialForms](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1)
- [Elixir School — Pattern Matching](https://elixirschool.com/en/lessons/basics/pattern_matching)
