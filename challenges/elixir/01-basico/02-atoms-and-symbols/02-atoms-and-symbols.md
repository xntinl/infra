# Atoms: The Transaction Status Type System

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. The `Transaction` module needs a type system for
payment statuses and error codes. In a language without enums, Elixir uses atoms
for exactly this purpose.

Project structure at this point:

```
payments_cli/
├── lib/
│   └── payments_cli/
│       ├── cli.ex              # from exercise 01
│       └── transaction.ex      # ← you implement this
├── test/
│   └── payments_cli/
│       └── transaction_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why this design decision matters

The payments system needs to represent transaction states: `:pending`, `:approved`,
`:declined`, `:reversed`, `:flagged`. You have two choices:

**Option A — Strings:**
```elixir
status = "approved"
```

**Option B — Atoms:**
```elixir
status = :approved
```

The difference is not just aesthetics. Atom comparison is O(1) — the VM compares
pointers into the global atom table, not byte sequences. With 10,000 transactions
being classified per second, string comparisons for status codes are measurable
overhead. More importantly, a typo in a string (`"aproved"`) compiles silently.
A typo in an atom used in a `case` clause can generate a compiler warning.

The `{:ok, value}` / `{:error, reason}` pattern appears in every Elixir API because
it makes error handling explicit and forces the caller to handle both cases. You
cannot accidentally ignore an `:error` the way you can ignore a `nil` return in
other languages.

---

## The business problem

The `Transaction` module needs:

1. A function `classify_status/1` that takes an atom status and returns a human-readable
   string category for reporting
2. A function `parse_status/1` that converts external string input (from CSV) to
   an atom status safely — without creating atoms from arbitrary strings

---

## Implementation

### `lib/payments_cli/transaction.ex`

```elixir
defmodule PaymentsCli.Transaction do
  @moduledoc """
  Represents a payment transaction and provides status classification.

  Status atoms are the canonical representation internally. External data
  (CSV, JSON, API responses) always arrives as strings and must be
  converted via parse_status/1 — never with String.to_atom/1.
  """

  # The complete set of valid statuses as a module attribute.
  # Defining them here means the compiler can warn on exhaustiveness
  # and documents the domain model in one place.
  @valid_statuses [:pending, :approved, :declined, :reversed, :flagged]

  @doc """
  Returns the list of valid transaction statuses.
  """
  @spec valid_statuses() :: [atom()]
  def valid_statuses, do: @valid_statuses

  @doc """
  Classifies a transaction status atom into a reporting category string.

  Returns a string category used in ledger reports.

  ## Examples

      iex> PaymentsCli.Transaction.classify_status(:approved)
      "successful"

      iex> PaymentsCli.Transaction.classify_status(:declined)
      "failed"

      iex> PaymentsCli.Transaction.classify_status(:flagged)
      "under_review"

  """
  @spec classify_status(atom()) :: String.t()
  def classify_status(status)

  # TODO: implement one clause per status atom
  #
  # HINT: use multiple function clauses with pattern matching:
  #   def classify_status(:approved), do: "successful"
  #   def classify_status(:reversed), do: "successful"   <- reversals clear successfully
  #   def classify_status(:declined), do: ...
  #   def classify_status(:flagged),  do: ...
  #   def classify_status(:pending),  do: ...
  #   def classify_status(unknown),   do: ...  <- catch-all for unknown atoms

  @doc """
  Parses a status string from external input (CSV, API) into a status atom.

  Returns {:ok, atom} for known statuses, {:error, :unknown_status} for anything else.
  Uses String.to_existing_atom/1 so it NEVER creates new atoms from external input.

  ## Examples

      iex> PaymentsCli.Transaction.parse_status("approved")
      {:ok, :approved}

      iex> PaymentsCli.Transaction.parse_status("hacked_value")
      {:error, :unknown_status}

  """
  @spec parse_status(String.t()) :: {:ok, atom()} | {:error, :unknown_status}
  def parse_status(string) when is_binary(string) do
    # TODO: implement safe status parsing
    #
    # HINT: use String.to_existing_atom/1 wrapped in a rescue block.
    # String.to_existing_atom/1 raises ArgumentError if the atom was never
    # defined anywhere in the loaded code. This prevents atom table exhaustion
    # from attacker-controlled input.
    #
    # After conversion, verify the atom is actually in @valid_statuses —
    # an attacker could send "ok" which IS an existing atom but not a valid status.
  end
end
```

### Given tests — must pass without modification

```elixir
# test/payments_cli/transaction_test.exs
defmodule PaymentsCli.TransactionTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Transaction

  describe "classify_status/1" do
    test "approved and reversed are both successful" do
      assert Transaction.classify_status(:approved) == "successful"
      assert Transaction.classify_status(:reversed) == "successful"
    end

    test "declined is failed" do
      assert Transaction.classify_status(:declined) == "failed"
    end

    test "flagged is under_review" do
      assert Transaction.classify_status(:flagged) == "under_review"
    end

    test "pending is pending" do
      assert Transaction.classify_status(:pending) == "pending"
    end

    test "unknown atom returns unknown category" do
      result = Transaction.classify_status(:some_future_status)
      assert is_binary(result)
    end
  end

  describe "parse_status/1" do
    test "parses all valid status strings" do
      for status <- Transaction.valid_statuses() do
        string = Atom.to_string(status)
        assert {:ok, ^status} = Transaction.parse_status(string)
      end
    end

    test "returns error for unknown status string" do
      assert {:error, :unknown_status} = Transaction.parse_status("hacked")
    end

    test "returns error for valid atom names that are not statuses" do
      # :ok is a real atom but not a valid transaction status
      assert {:error, :unknown_status} = Transaction.parse_status("ok")
    end

    test "returns error for empty string" do
      assert {:error, :unknown_status} = Transaction.parse_status("")
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/transaction_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Atoms (your impl) | Strings | Integer codes (e.g. 1, 2, 3) |
|--------|-------------------|---------|-------------------------------|
| Comparison speed | O(1) pointer compare | O(n) byte compare | O(1) |
| Typo safety | Compiler can warn | Silent failure | No semantic meaning |
| External input | Requires parse step | Direct use | Requires mapping |
| Memory | Global atom table, never GC'd | GC'd normally | Minimal |
| Exhaustiveness checking | Dialyzer + patterns | Nothing | Nothing |

Reflection question: `parse_status/1` uses `String.to_existing_atom/1` followed
by a membership check. Why are both steps needed? What attack does the membership
check prevent that `to_existing_atom` alone does not?

---

## Common production mistakes

**1. `String.to_atom/1` from external input**
Every unique string passed to `String.to_atom/1` creates a permanent entry in the
atom table. The BEAM atom table limit is 1,048,576 atoms. An attacker sending
unique status strings in API requests can exhaust it and crash the VM. Use
`String.to_existing_atom/1` plus a membership check, or a hardcoded `case` on strings.

**2. Using strings for internal status codes**
If your business logic pattern-matches on `"approved"` instead of `:approved`, you
lose O(1) comparison. You also introduce the risk of case mismatch (`"Approved"` vs
`"approved"`). Reserve strings for data that crosses system boundaries (serialization,
external APIs, user display).

**3. The atom table is global and process-independent**
Atoms created in one process are visible to all processes. Creating atoms from user
input in one handler affects the global atom table shared by the entire VM. There is
no per-process or per-request isolation.

**4. `true`, `false`, and `nil` are atoms**
`is_atom(true)` returns `true`. This surprises developers from other languages.
It means `:true == true` and you can use booleans in atom-typed fields. Do not
conflate them with your domain atoms.

**5. Atom ordering is alphabetical, not insertion order**
`[:declined, :approved, :pending] |> Enum.sort()` gives `[:approved, :declined, :pending]`.
Never rely on definition order when sorting atoms. Always sort explicitly.

---

## Resources

- [Atom — HexDocs](https://hexdocs.pm/elixir/Atom.html)
- [Erlang atom table limits — Erlang efficiency guide](https://www.erlang.org/doc/efficiency_guide/advanced.html)
- [String.to_existing_atom/1 — HexDocs](https://hexdocs.pm/elixir/String.html#to_existing_atom/1)
- [Elixir Getting Started — Atoms](https://elixir-lang.org/getting-started/basic-types.html#atoms)
