# Modules and Function Visibility: The payments_cli API Surface

**Project**: `payments_cli` — a CLI tool that processes payment transactions

---

## Project context

You are building `payments_cli`, a CLI tool that processes payment transactions from CSV
files, validates them, applies business rules, and produces ledger reports.

This exercise implements a `Config` module that demonstrates module design decisions:
what is public vs private, how module attributes work as compile-time constants, how
documentation becomes a first-class artifact with doctests, and how validated constructors
enforce configuration invariants. The module is completely self-contained.

---

## Why module design is an architectural decision

Every `def` in your module is a **contract**. Changing a public function signature
breaks every caller — potentially in other applications, other teams, other services.
Every `defp` is an implementation detail you can change freely.

The decision "should this be `def` or `defp`?" is not about access control in the
OO sense. It is about: **what is the stable interface, and what is the implementation?**

In a payments CLI, modules expose different surfaces:
- A `CLI` module might expose only `main/1` — the entire public API is one function
- A `Ledger` module exposes arithmetic functions — a stable math library
- A `Pipeline` module exposes `process_line/1` and `process_batch/1` — the entry points
- A `Router` module exposes routing and validation — business logic contracts

Internal helpers (like validation or CSV line building) should be `defp`. If you make
them `def` "just in case", you've created accidental contracts that you cannot change
without checking all callers.

Module attributes (`@`) are compile-time constants — not runtime variables. They are
ideal for configuration constants, version strings, and magic numbers that should
be named. They are the wrong tool for runtime configuration (use process state or ETS).

---

## The business problem

Implement a `Config` module that:

1. Exposes typed access to processing configuration (module attributes + public functions)
2. Documents all configuration options with `@doc` and doctests
3. Provides a validated constructor for configuration maps
4. Keeps validation logic private — it is an implementation detail

---

## Implementation

### `lib/payments_cli/config.ex`

The `Config` module uses module attributes for compile-time constants and exposes
them through typed public functions. `new/1` is the validated constructor — it
merges caller options with defaults, then runs validation. The validation logic
is private (`defp`) because it is an implementation detail. External callers use
`new/1` and get either `{:ok, config}` or `{:error, reason}`.

```elixir
defmodule PaymentsCli.Config do
  @moduledoc """
  Central configuration for the payments_cli processing system.

  All configuration values have documented defaults and validation.
  Use `PaymentsCli.Config.new/1` to create validated configurations.

  ## Usage

      iex> {:ok, config} = PaymentsCli.Config.new(fee_basis_points: 150)
      iex> config.fee_basis_points
      150

  """

  @default_fee_basis_points 250
  @default_max_amount_cents 1_000_000
  @supported_currencies ["USD", "EUR", "GBP", "JPY", "CAD"]
  @version "0.1.0"

  @doc """
  Returns the current version of payments_cli.

  ## Examples

      iex> PaymentsCli.Config.version()
      "0.1.0"

  """
  @spec version() :: String.t()
  def version, do: @version

  @doc """
  Returns the default fee in basis points (2.5%).

  One basis point = 0.01%. 250 basis points = 2.5%.

  ## Examples

      iex> PaymentsCli.Config.default_fee_basis_points()
      250

  """
  @spec default_fee_basis_points() :: non_neg_integer()
  def default_fee_basis_points, do: @default_fee_basis_points

  @doc """
  Returns the list of supported currency codes.

  ## Examples

      iex> "USD" in PaymentsCli.Config.supported_currencies()
      true

      iex> "XYZ" in PaymentsCli.Config.supported_currencies()
      false

  """
  @spec supported_currencies() :: [String.t()]
  def supported_currencies, do: @supported_currencies

  @doc """
  Creates a validated configuration map from keyword options.

  Accepted options:
    - fee_basis_points: integer >= 0 (default: 250)
    - max_amount_cents: integer > 0 (default: 1_000_000)
    - require_reference: boolean (default: false)
    - currencies: list of 3-char strings (default: all supported)

  Returns {:ok, config_map} or {:error, reason}.

  ## Examples

      iex> PaymentsCli.Config.new()
      {:ok, %{fee_basis_points: 250, max_amount_cents: 1_000_000, require_reference: false, currencies: ["USD", "EUR", "GBP", "JPY", "CAD"]}}

      iex> PaymentsCli.Config.new(fee_basis_points: -1)
      {:error, "fee_basis_points must be >= 0"}

  """
  @spec new(keyword()) :: {:ok, map()} | {:error, String.t()}
  def new(opts \\ []) when is_list(opts) do
    defaults = %{
      fee_basis_points: @default_fee_basis_points,
      max_amount_cents: @default_max_amount_cents,
      require_reference: false,
      currencies: @supported_currencies
    }

    config =
      Enum.reduce(opts, defaults, fn {key, value}, acc ->
        Map.put(acc, key, value)
      end)

    case validate_config(config) do
      :ok -> {:ok, config}
      {:error, reason} -> {:error, reason}
    end
  end

  @doc """
  Checks whether a currency code is supported by this configuration.

  ## Examples

      iex> {:ok, config} = PaymentsCli.Config.new()
      iex> PaymentsCli.Config.currency_supported?(config, "USD")
      true

      iex> {:ok, config} = PaymentsCli.Config.new()
      iex> PaymentsCli.Config.currency_supported?(config, "BTC")
      false

  """
  @spec currency_supported?(map(), String.t()) :: boolean()
  def currency_supported?(%{currencies: currencies}, currency) when is_binary(currency) do
    currency in currencies
  end

  # ---------------------------------------------------------------------------
  # Private — validation details are not part of the public contract
  # ---------------------------------------------------------------------------

  @spec validate_config(map()) :: :ok | {:error, String.t()}
  defp validate_config(%{fee_basis_points: fee}) when fee < 0 do
    {:error, "fee_basis_points must be >= 0"}
  end

  defp validate_config(%{max_amount_cents: max}) when max <= 0 do
    {:error, "max_amount_cents must be > 0"}
  end

  defp validate_config(%{currencies: currencies}) when not is_list(currencies) do
    {:error, "currencies must be a list"}
  end

  defp validate_config(%{currencies: []}), do: {:error, "currencies cannot be empty"}

  defp validate_config(_config), do: :ok
end
```

**Why this works:**

- Module attributes (`@default_fee_basis_points`, `@supported_currencies`, `@version`)
  are compile-time constants. They are inlined into the functions that reference them,
  so there is no runtime lookup cost. They name magic numbers and make the module
  self-documenting.

- `new/1` uses `Enum.reduce/3` to merge caller options into the defaults map. Each
  `{key, value}` pair from the keyword list overwrites the corresponding default.
  Unknown keys are silently added — this is intentional for forward compatibility.
  After merging, `validate_config/1` checks invariants.

- `validate_config/1` uses multiple `defp` clauses with guards. Each clause checks
  one invariant. The clauses are ordered so that the first failing check returns its
  error. The final catch-all returns `:ok`. Adding a new validation rule means adding
  one clause — no existing code changes.

- `currency_supported?/2` pattern-matches the `:currencies` key from the config map
  in the function head and uses `in` to check membership. The `in` operator works
  on lists and is O(n), but for a short list of 5 currencies, this is negligible.

### Tests

```elixir
# test/payments_cli/config_test.exs
defmodule PaymentsCli.ConfigTest do
  use ExUnit.Case, async: true

  alias PaymentsCli.Config

  doctest PaymentsCli.Config

  describe "version/0" do
    test "returns a version string" do
      assert is_binary(Config.version())
      assert Config.version() =~ ~r/\d+\.\d+\.\d+/
    end
  end

  describe "new/0 and new/1" do
    test "returns defaults with no opts" do
      assert {:ok, config} = Config.new()
      assert config.fee_basis_points == 250
      assert config.max_amount_cents == 1_000_000
      assert config.require_reference == false
      assert is_list(config.currencies)
    end

    test "custom fee basis points" do
      assert {:ok, config} = Config.new(fee_basis_points: 150)
      assert config.fee_basis_points == 150
      # Other defaults unchanged
      assert config.max_amount_cents == 1_000_000
    end

    test "returns error for negative fee" do
      assert {:error, message} = Config.new(fee_basis_points: -1)
      assert is_binary(message)
    end

    test "returns error for zero max_amount" do
      assert {:error, _} = Config.new(max_amount_cents: 0)
    end

    test "returns error for empty currencies list" do
      assert {:error, _} = Config.new(currencies: [])
    end
  end

  describe "currency_supported?/2" do
    setup do
      {:ok, config} = Config.new()
      {:ok, config: config}
    end

    test "returns true for supported currency", %{config: config} do
      assert Config.currency_supported?(config, "USD") == true
    end

    test "returns false for unsupported currency", %{config: config} do
      assert Config.currency_supported?(config, "BTC") == false
    end

    test "is case-sensitive", %{config: config} do
      assert Config.currency_supported?(config, "usd") == false
    end
  end

  describe "default_fee_basis_points/0 and supported_currencies/0" do
    test "basis points is a non-negative integer" do
      assert is_integer(Config.default_fee_basis_points())
      assert Config.default_fee_basis_points() >= 0
    end

    test "supported currencies is a list of 3-char strings" do
      currencies = Config.supported_currencies()
      assert is_list(currencies)
      assert Enum.all?(currencies, fn c -> is_binary(c) and String.length(c) == 3 end)
    end
  end
end
```

### Run the tests

```bash
mix test test/payments_cli/config_test.exs --trace
```

Note: `doctest PaymentsCli.Config` runs the examples in `@doc` blocks as tests.
Your `@doc` examples must be correct and runnable.

---

## Trade-off analysis

| Aspect | Module attributes `@attr` | Application config `Application.get_env/3` | Process state (Agent/GenServer) |
|--------|--------------------------|-------------------------------------------|--------------------------------|
| When evaluated | Compile time | Runtime (but app start) | Runtime |
| Can change at runtime | No | With `Application.put_env/3` | Yes |
| Testable | Yes — direct function calls | Requires Application setup | Requires process setup |
| Appropriate for | Named constants, magic numbers | Environment-specific config | Per-session state |

Reflection question: `validate_config/1` uses multiple `defp` clauses with guards.
What is the advantage of this approach over a single `defp` with a long `cond`?
Think about what happens when you add a new validation rule — which approach requires
fewer changes?

---

## Common production mistakes

**1. Using module attributes as runtime config**
```elixir
@base_url "https://api.example.com"  # WRONG for runtime config
# This is baked in at compile time. Changing the env variable has no effect.
```
Use `Application.get_env/3` or pass config explicitly. Module attributes are
for compile-time constants, not runtime configuration.

**2. Making private helpers public "just in case"**
Every `def` is a contract. If you later change `validate_config/1`'s behavior,
you break every caller. `defp` is not weakness — it is proper encapsulation.
If you want to test a private function, test it through the public API.

**3. `alias` does not change module loading**
`alias PaymentsCli.Ledger` makes `Ledger` available as a short name in the current
module. It does not load or compile `Ledger`. If `Ledger` is not in your project,
`alias` still works but calling `Ledger.sum_amounts/1` fails at runtime.

**4. Doctests fail silently when IEx output differs**
The doctest for `new/0` expects the map keys in a specific order. Map key order
in Elixir is not guaranteed for display. If the test fails with mismatched order,
adjust the doctest to use `{:ok, %{fee_basis_points: 250}} = Config.new()` instead
of expecting the full map.

**5. Module attributes in guards**
Module attributes are available in guards, but with a gotcha:
```elixir
@min 0
def valid?(n) when n >= @min, do: true  # OK — @min is substituted at compile time
```
This works because guards are evaluated at compile time. But computed attributes
(those using `@attr computed_value`) may not work as expected in guards.

---

## Resources

- [Modules — Elixir Getting Started](https://elixir-lang.org/getting-started/modules-and-functions.html)
- [Module attributes — Elixir Getting Started](https://elixir-lang.org/getting-started/module-attributes.html)
- [ExDoc — generate documentation](https://github.com/elixir-lang/ex_doc)
- [Doctests — ExUnit docs](https://hexdocs.pm/ex_unit/ExUnit.DocTest.html)
- [alias, require, import — Elixir Getting Started](https://elixir-lang.org/getting-started/alias-require-and-import.html)
