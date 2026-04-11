# Maps and Keyword Lists: Transaction Configuration

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. Transaction maps represent in-flight data.
Configuration for processing rules (fee rates, currency limits, retry policies)
uses keyword lists because order and optionality matter. Understanding when to use
each is a design decision, not a syntax question.

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
│       └── processor.ex    # ← you implement this
├── test/
│   └── payments_cli/
│       └── processor_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Maps vs keyword lists: the design question

Both maps and keyword lists associate keys with values. The choice between them
is determined by requirements, not preference:

**Use maps when:**
- Keys are unique (each transaction has one `amount_cents`)
- Access is O(1) by key
- Structure is fixed and known at compile time (or modeled with a struct in exercise 15)
- Data crosses module boundaries as the primary data type

**Use keyword lists when:**
- Options to a function need default values
- Key order matters to the caller
- Duplicate keys are meaningful (`headers: [:accept, :content_type]`)
- The list is passed as the last argument (Elixir convention for opts)

The Elixir standard library uses keyword lists for function options consistently:
`String.split(str, ",", trim: true, parts: 5)`. The `trim: true, parts: 5` part
is a keyword list. Your own option-accepting functions should follow the same pattern.

---

## The business problem

The `Processor` module needs to:

1. Apply processing rules to a transaction (using options as keyword lists)
2. Merge transaction updates immutably
3. Extract summary statistics from a transaction map
4. Validate that a transaction map has required fields

---

## Implementation

### `lib/payments_cli/processor.ex`

The `apply_rules/2` function merges caller options with defaults using
`Keyword.merge/2`. The order matters: `Keyword.merge(defaults, overrides)` gives
priority to `overrides`. After merging, the function validates business rules
and computes the fee.

`update_transaction/2` enforces that only existing fields can be updated — this
prevents accidental schema drift. It converts the keyword list to a map, checks
that all keys exist in the original transaction, then merges.

`summary/1` uses pattern matching in the function head to extract exactly the fields
needed for display. This is more explicit than `Map.take/2` and documents the expected
shape of the input at the function boundary.

```elixir
defmodule PaymentsCli.Processor do
  @moduledoc """
  Applies processing rules to transactions and manages transaction state updates.

  Processing options follow Elixir convention: keyword list as last argument
  with documented defaults. Callers only specify options they want to override.
  """

  @default_opts [
    fee_basis_points: 250,
    max_amount_cents: 1_000_000,
    require_reference: false
  ]

  @doc """
  Applies processing rules to a transaction map.

  Options:
    - fee_basis_points: integer, default 250 (2.5%)
    - max_amount_cents: integer, default 1_000_000 ($10,000)
    - require_reference: boolean, default false

  Returns {:ok, processed_transaction} or {:error, reason}.

  ## Examples

      iex> tx = %{id: "T1", amount_cents: 1000, status: :pending}
      iex> PaymentsCli.Processor.apply_rules(tx, fee_basis_points: 100)
      {:ok, %{id: "T1", amount_cents: 1000, fee_cents: 10, status: :pending}}

  """
  @spec apply_rules(map(), keyword()) :: {:ok, map()} | {:error, String.t()}
  def apply_rules(transaction, opts \\ []) when is_map(transaction) and is_list(opts) do
    effective_opts = Keyword.merge(@default_opts, opts)

    fee_bp = Keyword.get(effective_opts, :fee_basis_points)
    max_cents = Keyword.get(effective_opts, :max_amount_cents)
    require_ref = Keyword.get(effective_opts, :require_reference)

    cond do
      transaction.amount_cents > max_cents ->
        {:error, "amount #{transaction.amount_cents} exceeds maximum #{max_cents}"}

      require_ref and not Map.has_key?(transaction, :reference) ->
        {:error, "reference is required"}

      true ->
        fee = div(transaction.amount_cents * fee_bp, 10_000)
        {:ok, Map.put(transaction, :fee_cents, fee)}
    end
  end

  @doc """
  Updates a transaction map with new field values.

  Only allows updating fields that already exist in the transaction.
  Attempting to add new fields returns {:error, :unknown_fields}.

  This is the immutable update pattern — returns a new map, never mutates.

  ## Examples

      iex> tx = %{id: "T1", status: :pending, amount_cents: 1000}
      iex> PaymentsCli.Processor.update_transaction(tx, status: :approved)
      {:ok, %{id: "T1", status: :approved, amount_cents: 1000}}

      iex> PaymentsCli.Processor.update_transaction(tx, unknown_field: "value")
      {:error, :unknown_fields}

  """
  @spec update_transaction(map(), keyword()) :: {:ok, map()} | {:error, :unknown_fields}
  def update_transaction(transaction, updates) when is_map(transaction) and is_list(updates) do
    updates_map = Map.new(updates)
    unknown_keys = Map.keys(updates_map) -- Map.keys(transaction)

    if unknown_keys == [] do
      {:ok, Map.merge(transaction, updates_map)}
    else
      {:error, :unknown_fields}
    end
  end

  @doc """
  Extracts a summary map from a transaction with only the display-relevant fields.

  Returns a new map with only: id, amount_cents, currency, status.
  Other fields (internal references, fee_cents, etc.) are excluded.

  ## Examples

      iex> tx = %{id: "T1", amount_cents: 500, currency: "USD", status: :approved, fee_cents: 12, internal_ref: "X"}
      iex> PaymentsCli.Processor.summary(tx)
      %{id: "T1", amount_cents: 500, currency: "USD", status: :approved}

  """
  @spec summary(map()) :: map()
  def summary(%{id: id, amount_cents: amount, currency: currency, status: status}) do
    %{id: id, amount_cents: amount, currency: currency, status: status}
  end
end
```

**Why this works:**

- `apply_rules/2` uses `Keyword.merge(@default_opts, opts)` — the second argument's
  values win on conflict. This means caller-provided options override defaults, which
  is the standard convention. `Keyword.merge(opts, @default_opts)` would be backwards —
  defaults would override caller options.

- `update_transaction/2` converts the keyword list to a map with `Map.new/1`, then
  computes the difference between update keys and transaction keys using `--`. If any
  keys in the update are not in the original transaction, the function rejects the
  update. This prevents typos like `staus: :approved` from silently adding a new field.

- `summary/1` pattern-matches four specific keys in the function head. If the input
  map is missing any of these keys, the match fails with a `FunctionClauseError` — this
  is fail-fast behavior that immediately surfaces schema mismatches.

### Given tests — must pass without modification

```elixir
# test/payments_cli/processor_test.exs
defmodule PaymentsCli.ProcessorTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Processor

  @base_tx %{id: "TXN001", amount_cents: 1000, currency: "USD", status: :pending}

  describe "apply_rules/2" do
    test "applies default fee when no opts given" do
      assert {:ok, tx} = Processor.apply_rules(@base_tx)
      # Default fee: 2.5% of 1000 cents = 25 cents
      assert tx.fee_cents == 25
    end

    test "applies custom fee basis points" do
      assert {:ok, tx} = Processor.apply_rules(@base_tx, fee_basis_points: 100)
      # 1% of 1000 cents = 10 cents
      assert tx.fee_cents == 10
    end

    test "returns error when amount exceeds maximum" do
      assert {:error, _reason} = Processor.apply_rules(@base_tx, max_amount_cents: 500)
    end

    test "returns error when reference required but missing" do
      assert {:error, _reason} = Processor.apply_rules(@base_tx, require_reference: true)
    end

    test "allows transaction when reference required and present" do
      tx_with_ref = Map.put(@base_tx, :reference, "REF001")
      assert {:ok, _tx} = Processor.apply_rules(tx_with_ref, require_reference: true)
    end
  end

  describe "update_transaction/2" do
    test "updates an existing field" do
      assert {:ok, tx} = Processor.update_transaction(@base_tx, status: :approved)
      assert tx.status == :approved
      # Other fields unchanged
      assert tx.id == "TXN001"
      assert tx.amount_cents == 1000
    end

    test "returns error for unknown field" do
      assert {:error, :unknown_fields} =
               Processor.update_transaction(@base_tx, new_field: "value")
    end

    test "does not mutate the original" do
      {:ok, _updated} = Processor.update_transaction(@base_tx, status: :approved)
      assert @base_tx.status == :pending
    end
  end

  describe "summary/1" do
    test "returns only display fields" do
      full_tx = Map.merge(@base_tx, %{fee_cents: 25, internal_ref: "REF", extra: "data"})
      result = Processor.summary(full_tx)
      assert Map.keys(result) |> Enum.sort() == [:amount_cents, :currency, :id, :status]
    end

    test "contains correct values" do
      result = Processor.summary(@base_tx)
      assert result.id == "TXN001"
      assert result.status == :pending
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/processor_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Maps (transactions) | Keyword lists (opts) | Structs (exercise 15) |
|--------|--------------------|--------------------|----------------------|
| Key uniqueness | Enforced | Not enforced | Enforced at compile time |
| Access speed | O(1) | O(n) — linear scan | O(1) — same as map |
| Default values | Manual with `Map.get/3` | `Keyword.get/3` with default | `defstruct` field defaults |
| Type checking | None | None | Pattern match by module |
| Adding unknown fields | Always allowed | N/A | Compile-time error |

Reflection question: `apply_rules/2` uses `Keyword.merge(@default_opts, opts)` to
merge options. What is the difference between `Keyword.merge(defaults, overrides)` and
`Keyword.merge(overrides, defaults)`? Which one gives you "caller overrides defaults"
behavior, and why?

---

## Common production mistakes

**1. Dot notation on string-keyed maps**
`config = %{"host" => "localhost"}` — then `config.host` raises `KeyError`.
Dot notation only works with atom keys. Use `config["host"]` or `Map.get/3`.

**2. `%{map | key: val}` to add a new key**
`%{tx | new_key: value}` raises `KeyError` if `:new_key` does not exist.
The update syntax only modifies existing keys. Use `Map.put/3` to add.

**3. Assuming map key order**
`%{b: 2, a: 1}` may print as `%{a: 1, b: 2}` in IEx (sorted for display)
but iteration order is not guaranteed. Never rely on `Map.keys/1` being sorted
unless you call `Enum.sort/1` explicitly.

**4. Keyword list with `[]` access vs `Keyword.get/3`**
`opts[:missing_key]` returns `nil`. `Keyword.get(opts, :missing_key, default)` returns
`default`. If your default is not `nil`, use `Keyword.get/3` explicitly — silence nil
returns are hard to debug.

**5. Mutating a map with `Map.put/3` and discarding the result**
```elixir
Map.put(transaction, :status, :approved)  # BAD: result discarded
transaction  # still has the old status
```
Maps are immutable. `Map.put/3` returns a new map. Always bind the result.

---

## Resources

- [Map — HexDocs](https://hexdocs.pm/elixir/Map.html)
- [Keyword — HexDocs](https://hexdocs.pm/elixir/Keyword.html)
- [Elixir Getting Started — Keywords and Maps](https://elixir-lang.org/getting-started/keywords-and-maps.html)
- [Elixir School — Maps](https://elixirschool.com/en/lessons/basics/collections#maps-6)
