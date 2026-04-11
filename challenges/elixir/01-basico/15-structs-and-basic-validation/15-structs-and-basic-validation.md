# Structs and Basic Validation: Formalizing the Transaction Type

**Project**: `payments_cli` — built incrementally across the basic level

---

## Project context

You're building `payments_cli`. After fourteen exercises, the transaction data has
been passed around as plain maps: `%{id: "T1", status: :approved, amount_cents: 1000, ...}`.
That works, but it has a critical weakness: nothing prevents a caller from passing
`%{id: "T1", amount: 1000}` (forgetting `amount_cents`) or `%{status: "approved"}`
(wrong type for status). The compiler cannot help — it has no idea what shape a
"transaction" should have.

Structs fix this. A `%PaymentsCli.Transaction{}` struct declares the exact fields
at compile time, provides defaults, and enables pattern matching by type. This exercise
replaces the ad-hoc map convention with a proper typed struct, giving the entire
project a stable, documented contract for what a transaction is.

Project structure at this point:

```
payments_cli/
├── lib/
│   └── payments_cli/
│       ├── cli.ex
│       ├── transaction.ex      # ← you extend this (add defstruct)
│       ├── ledger.ex
│       ├── formatter.ex
│       ├── pipeline.ex
│       ├── processor.ex
│       ├── router.ex
│       ├── analytics.ex
│       ├── report.ex
│       ├── rules.ex
│       └── config.ex
├── test/
│   └── payments_cli/
│       └── transaction_struct_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why structs are an architectural decision, not syntax sugar

Up to this point, the project has used convention to communicate what a transaction
looks like. Every module has a comment like `# transactions are maps with :id, :status,
:amount_cents, :currency`. This is documentation — invisible to the compiler.

A struct changes that:

1. **Compile-time field enforcement**: `%PaymentsCli.Transaction{nonexistent: 1}` is a
   compile error. With maps, `%{nonexistent: 1}` silently creates an unintended key.

2. **Pattern matching by type**: `def process(%Transaction{} = tx)` will raise at
   runtime if the caller passes a plain map. The function signature becomes a contract
   enforced at the call boundary.

3. **`@spec` alignment**: type specs can reference `%Transaction{}` rather than `map()`,
   enabling Dialyzer to catch misuse.

4. **Defaults are documented in the code**: `defstruct amount_cents: 0` documents the
   default at the definition site, not scattered across callers.

The trade-off: structs are more rigid. If you need a general key-value bag (arbitrary
keys, flexible shape), a map is correct. When the shape is known and fixed — a domain
entity like a transaction — a struct is the right tool.

---

## The business problem

The `PaymentsCli.Transaction` module already has functions (`classify_status/1`,
`parse_status/1`, etc.) that work on maps. This exercise adds a `defstruct` to that
module, a validated `new/1` constructor, and functions that leverage pattern matching
by struct type.

The new `%Transaction{}` struct captures all the fields that the rest of the project
has been using by convention: `:id`, `:status`, `:amount_cents`, `:currency`,
`:merchant`, `:date`, and `:reference`.

---

## Implementation

### Extend `lib/payments_cli/transaction.ex`

The struct uses `@enforce_keys` for required fields and provides defaults for
optional fields. The `new/1` constructor validates required fields are present
before building the struct, then runs validation on the built struct. This two-step
approach catches both missing fields and invalid values.

`approved?/1` and `set_status/2` pattern-match on `%__MODULE__{}` in their function
heads — they only accept Transaction structs, not plain maps. This is type safety
enforced at the function boundary.

```elixir
defmodule PaymentsCli.Transaction do
  @moduledoc """
  Typed representation of a payment transaction.

  Use `new/1` to create validated transactions.
  All processing functions in payments_cli accept `%Transaction{}` structs.

  ## Examples

      iex> {:ok, tx} = PaymentsCli.Transaction.new(id: "T1", amount_cents: 1000, currency: "USD")
      iex> tx.status
      :pending

  """

  @enforce_keys [:id, :amount_cents, :currency]
  defstruct [
    :id,
    :amount_cents,
    :currency,
    status: :pending,
    merchant: nil,
    date: nil,
    reference: nil
  ]

  @type t :: %__MODULE__{
    id: String.t(),
    amount_cents: non_neg_integer(),
    currency: String.t(),
    status: :pending | :approved | :declined | :flagged,
    merchant: String.t() | nil,
    date: String.t() | nil,
    reference: String.t() | nil
  }

  @valid_statuses [:pending, :approved, :declined, :reversed, :flagged]

  @doc """
  Returns the list of valid transaction statuses.
  """
  @spec valid_statuses() :: [atom()]
  def valid_statuses, do: @valid_statuses

  @doc """
  Classifies a transaction status atom into a reporting category string.

  ## Examples

      iex> PaymentsCli.Transaction.classify_status(:approved)
      "successful"

      iex> PaymentsCli.Transaction.classify_status(:declined)
      "failed"

      iex> PaymentsCli.Transaction.classify_status(:flagged)
      "under_review"

  """
  @spec classify_status(atom()) :: String.t()
  def classify_status(:approved), do: "successful"
  def classify_status(:reversed), do: "successful"
  def classify_status(:declined), do: "failed"
  def classify_status(:flagged), do: "under_review"
  def classify_status(:pending), do: "pending"
  def classify_status(_unknown), do: "unknown"

  @doc """
  Parses a status string from external input into a status atom.

  ## Examples

      iex> PaymentsCli.Transaction.parse_status("approved")
      {:ok, :approved}

      iex> PaymentsCli.Transaction.parse_status("hacked_value")
      {:error, :unknown_status}

  """
  @spec parse_status(String.t()) :: {:ok, atom()} | {:error, :unknown_status}
  def parse_status(string) when is_binary(string) do
    atom =
      try do
        String.to_existing_atom(string)
      rescue
        ArgumentError -> nil
      end

    if atom in @valid_statuses do
      {:ok, atom}
    else
      {:error, :unknown_status}
    end
  end

  @doc """
  Creates a validated Transaction struct from a keyword list.

  Required fields: `:id`, `:amount_cents`, `:currency`
  Optional fields (with defaults): `:status` (`:pending`), `:merchant`, `:date`, `:reference`

  Returns `{:ok, %Transaction{}}` on success, `{:error, reason}` on failure.

  ## Examples

      iex> PaymentsCli.Transaction.new(id: "T1", amount_cents: 500, currency: "USD")
      {:ok, %PaymentsCli.Transaction{id: "T1", amount_cents: 500, currency: "USD", status: :pending, merchant: nil, date: nil, reference: nil}}

      iex> PaymentsCli.Transaction.new(id: "T1", amount_cents: -1, currency: "USD")
      {:error, "amount_cents must be >= 0"}

      iex> PaymentsCli.Transaction.new(amount_cents: 500, currency: "USD")
      {:error, "id is required"}

  """
  @spec new(keyword()) :: {:ok, t()} | {:error, String.t()}
  def new(fields) when is_list(fields) do
    with :ok <- check_required(fields, :id),
         :ok <- check_required(fields, :amount_cents),
         :ok <- check_required(fields, :currency) do
      tx = struct(__MODULE__, fields)

      case validate(tx) do
        :ok -> {:ok, tx}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  @doc """
  Returns true if the transaction has been approved.

  ## Examples

      iex> {:ok, tx} = PaymentsCli.Transaction.new(id: "T1", amount_cents: 100, currency: "USD", status: :approved)
      iex> PaymentsCli.Transaction.approved?(tx)
      true

      iex> {:ok, tx} = PaymentsCli.Transaction.new(id: "T2", amount_cents: 100, currency: "USD")
      iex> PaymentsCli.Transaction.approved?(tx)
      false

  """
  @spec approved?(t()) :: boolean()
  def approved?(%__MODULE__{status: :approved}), do: true
  def approved?(%__MODULE__{}), do: false

  @doc """
  Updates the status of a transaction, returning a new struct.

  Returns `{:error, :invalid_status}` if the status is not a known atom.

  ## Examples

      iex> {:ok, tx} = PaymentsCli.Transaction.new(id: "T1", amount_cents: 100, currency: "USD")
      iex> {:ok, approved} = PaymentsCli.Transaction.set_status(tx, :approved)
      iex> approved.status
      :approved

      iex> {:ok, tx} = PaymentsCli.Transaction.new(id: "T1", amount_cents: 100, currency: "USD")
      iex> PaymentsCli.Transaction.set_status(tx, :unknown)
      {:error, :invalid_status}

  """
  @spec set_status(t(), atom()) :: {:ok, t()} | {:error, :invalid_status}
  def set_status(%__MODULE__{} = tx, status)
      when status in [:pending, :approved, :declined, :flagged] do
    {:ok, %__MODULE__{tx | status: status}}
  end

  def set_status(%__MODULE__{}, _status), do: {:error, :invalid_status}

  @doc """
  Formats the transaction amount as a human-readable dollar string.

  ## Examples

      iex> {:ok, tx} = PaymentsCli.Transaction.new(id: "T1", amount_cents: 15099, currency: "USD")
      iex> PaymentsCli.Transaction.format_amount(tx)
      "$150.99"

  """
  @spec format_amount(t()) :: String.t()
  def format_amount(%__MODULE__{amount_cents: cents}) do
    major = div(cents, 100)
    minor = rem(cents, 100)
    "$#{major}.#{minor |> Integer.to_string() |> String.pad_leading(2, "0")}"
  end

  # ---------------------------------------------------------------------------
  # Private — validation details are implementation, not public contract
  # ---------------------------------------------------------------------------

  @spec check_required(keyword(), atom()) :: :ok | {:error, String.t()}
  defp check_required(fields, key) do
    if Keyword.has_key?(fields, key) do
      :ok
    else
      {:error, "#{key} is required"}
    end
  end

  @spec validate(t()) :: :ok | {:error, String.t()}
  defp validate(%__MODULE__{amount_cents: cents}) when cents < 0 do
    {:error, "amount_cents must be >= 0"}
  end

  defp validate(%__MODULE__{currency: currency})
       when not is_binary(currency) or byte_size(currency) == 0 do
    {:error, "currency must be a non-empty string"}
  end

  defp validate(%__MODULE__{id: id}) when not is_binary(id) or byte_size(id) == 0 do
    {:error, "id must be a non-empty string"}
  end

  defp validate(%__MODULE__{}), do: :ok
end
```

**Why this works:**

- `@enforce_keys [:id, :amount_cents, :currency]` makes `%Transaction{}` raise
  `ArgumentError` if any of these fields is missing when the struct is created directly.
  However, `struct/2` (used in `new/1`) does not enforce these keys — it silently
  defaults missing keys to `nil`. That is why `new/1` checks required fields explicitly
  with `check_required/2` before calling `struct/2`.

- `new/1` uses `with` to chain three required-field checks. If any check fails,
  `with` returns `{:error, "field is required"}` immediately. After building the struct,
  `validate/1` checks value constraints (non-negative amount, non-empty strings).

- `approved?/1` and `set_status/2` pattern-match on `%__MODULE__{}` — they only accept
  Transaction structs. A plain map `%{status: :approved}` will not match, producing
  a `FunctionClauseError`. This is type safety at the function boundary.

- `set_status/2` uses a guard `when status in [:pending, :approved, :declined, :flagged]`
  to restrict valid statuses. The catch-all clause returns `{:error, :invalid_status}`.

- `format_amount/1` extracts `amount_cents` from the struct in the function head,
  splits into dollars and cents, and formats with a leading zero on the cents part.

### Given tests — must pass without modification

```elixir
# test/payments_cli/transaction_struct_test.exs
defmodule PaymentsCli.TransactionStructTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Transaction

  doctest PaymentsCli.Transaction

  describe "new/1" do
    test "creates a transaction with required fields" do
      assert {:ok, tx} = Transaction.new(id: "T1", amount_cents: 1000, currency: "USD")
      assert tx.id == "T1"
      assert tx.amount_cents == 1000
      assert tx.currency == "USD"
    end

    test "applies default status of :pending" do
      assert {:ok, tx} = Transaction.new(id: "T1", amount_cents: 500, currency: "USD")
      assert tx.status == :pending
    end

    test "accepts optional fields" do
      assert {:ok, tx} =
               Transaction.new(
                 id: "T1",
                 amount_cents: 500,
                 currency: "USD",
                 merchant: "Coffee Co",
                 date: "2024-01-15",
                 reference: "REF-001"
               )

      assert tx.merchant == "Coffee Co"
      assert tx.date == "2024-01-15"
      assert tx.reference == "REF-001"
    end

    test "returns error when id is missing" do
      assert {:error, message} = Transaction.new(amount_cents: 500, currency: "USD")
      assert is_binary(message)
    end

    test "returns error when amount_cents is missing" do
      assert {:error, message} = Transaction.new(id: "T1", currency: "USD")
      assert is_binary(message)
    end

    test "returns error when currency is missing" do
      assert {:error, message} = Transaction.new(id: "T1", amount_cents: 500)
      assert is_binary(message)
    end

    test "returns error for negative amount_cents" do
      assert {:error, message} = Transaction.new(id: "T1", amount_cents: -1, currency: "USD")
      assert is_binary(message)
    end

    test "returns error for empty id" do
      assert {:error, _} = Transaction.new(id: "", amount_cents: 500, currency: "USD")
    end

    test "returns error for empty currency" do
      assert {:error, _} = Transaction.new(id: "T1", amount_cents: 500, currency: "")
    end
  end

  describe "approved?/1" do
    test "returns true for approved transaction" do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 100, currency: "USD", status: :approved)
      assert Transaction.approved?(tx) == true
    end

    test "returns false for pending transaction" do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 100, currency: "USD")
      assert Transaction.approved?(tx) == false
    end

    test "returns false for declined transaction" do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 100, currency: "USD", status: :declined)
      assert Transaction.approved?(tx) == false
    end
  end

  describe "set_status/2" do
    setup do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 100, currency: "USD")
      {:ok, tx: tx}
    end

    test "updates status to approved", %{tx: tx} do
      assert {:ok, updated} = Transaction.set_status(tx, :approved)
      assert updated.status == :approved
    end

    test "does not mutate the original struct", %{tx: tx} do
      {:ok, _updated} = Transaction.set_status(tx, :approved)
      assert tx.status == :pending
    end

    test "returns error for unknown status", %{tx: tx} do
      assert {:error, :invalid_status} = Transaction.set_status(tx, :unknown)
    end
  end

  describe "format_amount/1" do
    test "formats cents as dollar string" do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 15099, currency: "USD")
      assert Transaction.format_amount(tx) == "$150.99"
    end

    test "pads single-digit cents" do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 100, currency: "USD")
      assert Transaction.format_amount(tx) == "$1.00"
    end

    test "handles zero" do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 0, currency: "USD")
      assert Transaction.format_amount(tx) == "$0.00"
    end
  end

  describe "struct type safety" do
    test "is_struct/2 verifies module type" do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 100, currency: "USD")
      assert is_struct(tx, Transaction)
      refute is_struct(%{id: "T1", amount_cents: 100, currency: "USD"}, Transaction)
    end

    test "struct is also a map" do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 100, currency: "USD")
      assert is_map(tx)
    end

    test "immutable update creates new struct" do
      {:ok, tx} = Transaction.new(id: "T1", amount_cents: 100, currency: "USD")
      updated = %Transaction{tx | amount_cents: 200}
      assert tx.amount_cents == 100
      assert updated.amount_cents == 200
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/transaction_struct_test.exs --trace
```

Note: `doctest PaymentsCli.Transaction` runs the examples in `@doc` blocks as tests.
Your `@doc` examples must produce the exact output shown.

---

## Trade-off analysis

| Aspect | Plain map `%{...}` | Struct `%Transaction{}` | Ecto.Schema |
|--------|-------------------|------------------------|-------------|
| Compile-time field check | No | Yes (with `@enforce_keys`) | Yes |
| Pattern match by type | No | Yes | Yes |
| Can add arbitrary keys | Yes | No (`KeyError`) | No |
| Type spec support | `map()` only | `%Transaction{}` | `%Transaction{}` |
| Changeset / validation | Manual | Manual (`new/1`) | Built-in |
| Appropriate for | Exploratory code, flexible shapes | Domain entities with known fields | Database-backed entities |

Reflection question: `new/1` uses `struct/2` to build the transaction, which silently
ignores unknown keys. An alternative is to check for unknown keys and return
`{:error, "unknown field: #{key}"}` — rejecting unexpected input explicitly.
When would that stricter approach prevent real bugs? When would it make the API
unnecessarily fragile?

---

## Common production mistakes

**1. `@enforce_keys` without handling the `ArgumentError`**
```elixir
@enforce_keys [:id]
defstruct [:id, name: ""]
```
If you create `%MyStruct{}` without `:id`, Elixir raises `ArgumentError` at compile
time (in `defstruct` context) or at runtime (when `struct!` is called directly).
Always provide `:id` or use your `new/1` constructor which validates before creating.

**2. Updating a field that does not exist**
```elixir
tx = %Transaction{id: "T1", amount_cents: 100, currency: "USD"}
%Transaction{tx | fee: 25}  # KeyError — :fee is not a defined field
```
The update syntax `%Struct{original | key: val}` can only modify existing fields.
Adding a new field requires changing `defstruct`. This is intentional — it prevents
accidental schema drift.

**3. Pattern matching structs from different modules**
```elixir
# Two modules, both with a :name field
defmodule Cat do defstruct name: "" end
defmodule Dog do defstruct name: "" end

def pet_name(%Cat{name: n}), do: n  # only matches Cat structs
def pet_name(%Dog{name: n}), do: n  # only matches Dog structs
```
The `%ModuleName{}` pattern checks the `__struct__` key of the map, which stores
the module atom. A `%Cat{}` will never match `%Dog{name: n}` even though both have
`:name`. This type-safety is the main advantage of structs over maps.

**4. Assuming struct equality is field equality**
```elixir
%Transaction{id: "T1", amount_cents: 100, currency: "USD"} ==
%Transaction{id: "T1", amount_cents: 100, currency: "USD"}
# => true — struct equality IS field equality (including __struct__ key)
```
This actually works correctly — struct equality compares all fields including the
hidden `__struct__` field. But be careful: a struct and a map with the same visible
fields are NOT equal, because the map lacks `__struct__`.

**5. Using `Map.put/3` to add fields to structs**
```elixir
tx = %Transaction{id: "T1", amount_cents: 100, currency: "USD"}
Map.put(tx, :extra_field, "value")
# => %{__struct__: PaymentsCli.Transaction, id: "T1", ..., extra_field: "value"}
```
`Map.put/3` bypasses struct validation and adds the field anyway (it treats the
struct as a plain map). The result is no longer a valid struct — it has an extra
key that `defstruct` did not declare. Pattern matching `%Transaction{}` on it still
works (the `__struct__` key is present), but the extra field is invisible to struct
operations and may confuse the next reader. Never use `Map.put/3` to modify structs.

---

## Resources

- [Structs — Elixir Getting Started](https://elixir-lang.org/getting-started/structs.html)
- [defstruct — Kernel docs](https://hexdocs.pm/elixir/Kernel.html#defstruct/1)
- [@enforce_keys — Kernel docs](https://hexdocs.pm/elixir/Kernel.html#module-enforcing-keys)
- [struct/2 — Kernel docs](https://hexdocs.pm/elixir/Kernel.html#struct/2)
- [Elixir School — Structs](https://elixirschool.com/en/lessons/basics/structs)
- [Typespec for structs — Elixir Getting Started](https://elixir-lang.org/getting-started/typespecs-and-behaviours.html)
