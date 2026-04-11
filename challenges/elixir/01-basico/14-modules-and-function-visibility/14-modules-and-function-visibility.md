# Modules and Function Visibility: Building a Payment Gateway Adapter

**Project**: `pay_adapter` — a payment gateway with clean public API and private HTTP internals

---

## Why visibility is an architectural decision

In Elixir, `def` creates a public function visible to any module. `defp` creates
a private function visible only within the defining module. This is not just access
control — it is a contract:

- **Public functions** (`def`) are the module's API. Other modules depend on them.
  Changing a public function's signature or behavior is a breaking change.
- **Private functions** (`defp`) are implementation details. You can rename, rewrite,
  or delete them without affecting any other module.

This distinction matters in production because:

1. It documents intent: "this function is safe to call" vs "this is internal"
2. It limits the blast radius of changes: refactor private functions freely
3. It enables compiler warnings: calling a private function from another module
   is a compile error, not a runtime error

Module attributes (`@moduledoc`, `@doc`, `@spec`, custom `@` attributes) are
compile-time constants. They are evaluated at compile time and embedded in the
module's bytecode — not runtime variables.

---

## The business problem

Build a payment gateway adapter that:

1. Exposes a clean public API: `charge/2`, `refund/2`, `status/1`
2. Hides HTTP details, request building, and response parsing as private functions
3. Uses module attributes for configuration (base URL, timeouts, API version)
4. Demonstrates `alias`, `import`, and `use` for module composition
5. Follows the Adapter pattern: same public API, swappable implementations

---

## Implementation

### `lib/pay_adapter.ex`

```elixir
defmodule PayAdapter do
  @moduledoc """
  Payment gateway adapter with a clean public API.

  The public functions (charge, refund, status) define the contract
  that the rest of the application depends on. The private functions
  handle HTTP communication, request building, and response parsing.

  In production, the HTTP client would be injected for testability.
  Here we simulate it to focus on module organization patterns.
  """

  @base_url "https://api.payments.example.com/v1"
  @timeout_ms 5_000
  @api_version "2024-01-01"
  @max_retries 3

  @doc """
  Charges a payment method for the given amount.

  This is part of the public API. Other modules call this function.
  It delegates to private functions for request building and response parsing.

  ## Examples

      iex> PayAdapter.charge("pm_visa_4242", %{amount: 2500, currency: "usd", description: "Order #123"})
      {:ok, %{id: "ch_" <> _, status: :succeeded, amount: 2500}}

  """
  @spec charge(String.t(), map()) :: {:ok, map()} | {:error, term()}
  def charge(payment_method_id, params) when is_binary(payment_method_id) and is_map(params) do
    with :ok <- validate_charge_params(params),
         body <- build_charge_request(payment_method_id, params),
         {:ok, response} <- http_post("/charges", body) do
      parse_charge_response(response)
    end
  end

  @doc """
  Refunds a previous charge.

  ## Examples

      iex> PayAdapter.refund("ch_abc123", %{amount: 1000, reason: "customer_request"})
      {:ok, %{id: "rf_" <> _, status: :pending, amount: 1000}}

  """
  @spec refund(String.t(), map()) :: {:ok, map()} | {:error, term()}
  def refund(charge_id, params \\ %{}) when is_binary(charge_id) do
    with body <- build_refund_request(charge_id, params),
         {:ok, response} <- http_post("/refunds", body) do
      parse_refund_response(response)
    end
  end

  @doc """
  Checks the status of a charge.

  ## Examples

      iex> PayAdapter.status("ch_abc123")
      {:ok, %{id: "ch_abc123", status: :succeeded}}

  """
  @spec status(String.t()) :: {:ok, map()} | {:error, term()}
  def status(charge_id) when is_binary(charge_id) do
    case http_get("/charges/#{charge_id}") do
      {:ok, response} -> parse_status_response(charge_id, response)
      {:error, reason} -> {:error, reason}
    end
  end

  @doc """
  Returns the configured base URL. Useful for debugging.
  """
  @spec base_url() :: String.t()
  def base_url, do: @base_url

  @doc """
  Returns the API version this adapter targets.
  """
  @spec api_version() :: String.t()
  def api_version, do: @api_version

  # ---------------------------------------------------------------------------
  # Private — validation (internal contract enforcement)
  # ---------------------------------------------------------------------------

  @spec validate_charge_params(map()) :: :ok | {:error, String.t()}
  defp validate_charge_params(params) do
    cond do
      not is_integer(params[:amount]) or params[:amount] <= 0 ->
        {:error, "amount must be a positive integer (cents)"}

      not is_binary(params[:currency]) or byte_size(params[:currency]) != 3 ->
        {:error, "currency must be a 3-letter ISO code"}

      true ->
        :ok
    end
  end

  # ---------------------------------------------------------------------------
  # Private — request building (knows the API shape)
  # ---------------------------------------------------------------------------

  @spec build_charge_request(String.t(), map()) :: map()
  defp build_charge_request(payment_method_id, params) do
    %{
      payment_method: payment_method_id,
      amount: params[:amount],
      currency: params[:currency],
      description: params[:description],
      api_version: @api_version
    }
  end

  @spec build_refund_request(String.t(), map()) :: map()
  defp build_refund_request(charge_id, params) do
    base = %{charge: charge_id, api_version: @api_version}

    if params[:amount] do
      Map.put(base, :amount, params[:amount])
    else
      base
    end
  end

  # ---------------------------------------------------------------------------
  # Private — HTTP communication (simulated for this exercise)
  # ---------------------------------------------------------------------------
  # In production, these would use HTTPoison, Finch, or Req.
  # The simulation lets us focus on module design without network deps.

  @spec http_post(String.t(), map()) :: {:ok, map()} | {:error, term()}
  defp http_post(path, body) do
    _url = @base_url <> path
    _timeout = @timeout_ms
    _retries = @max_retries

    # Simulated response based on the path
    case path do
      "/charges" ->
        {:ok, %{id: generate_id("ch"), status: "succeeded", amount: body[:amount]}}

      "/refunds" ->
        {:ok, %{id: generate_id("rf"), status: "pending", amount: body[:amount] || 0}}

      _ ->
        {:error, :not_found}
    end
  end

  @spec http_get(String.t()) :: {:ok, map()} | {:error, term()}
  defp http_get(path) do
    _url = @base_url <> path

    case path do
      "/charges/" <> _id ->
        {:ok, %{status: "succeeded"}}

      _ ->
        {:error, :not_found}
    end
  end

  # ---------------------------------------------------------------------------
  # Private — response parsing (translates API shapes to domain shapes)
  # ---------------------------------------------------------------------------

  @spec parse_charge_response(map()) :: {:ok, map()} | {:error, term()}
  defp parse_charge_response(%{id: id, status: status, amount: amount}) do
    {:ok,
     %{
       id: id,
       status: parse_api_status(status),
       amount: amount
     }}
  end

  defp parse_charge_response(_), do: {:error, :unexpected_response}

  @spec parse_refund_response(map()) :: {:ok, map()} | {:error, term()}
  defp parse_refund_response(%{id: id, status: status, amount: amount}) do
    {:ok,
     %{
       id: id,
       status: parse_api_status(status),
       amount: amount
     }}
  end

  defp parse_refund_response(_), do: {:error, :unexpected_response}

  @spec parse_status_response(String.t(), map()) :: {:ok, map()}
  defp parse_status_response(charge_id, %{status: status}) do
    {:ok, %{id: charge_id, status: parse_api_status(status)}}
  end

  @spec parse_api_status(String.t()) :: atom()
  defp parse_api_status("succeeded"), do: :succeeded
  defp parse_api_status("pending"), do: :pending
  defp parse_api_status("failed"), do: :failed
  defp parse_api_status(_), do: :unknown

  @spec generate_id(String.t()) :: String.t()
  defp generate_id(prefix) do
    suffix =
      :crypto.strong_rand_bytes(8)
      |> Base.url_encode64(padding: false)

    "#{prefix}_#{suffix}"
  end
end
```

### `lib/pay_adapter/receipt.ex`

```elixir
defmodule PayAdapter.Receipt do
  @moduledoc """
  Formats payment results for display.

  Demonstrates `alias` and how child modules organize related
  functionality under a namespace without inheritance.
  """

  alias PayAdapter

  @doc """
  Formats a charge result into a receipt string.

  ## Examples

      iex> PayAdapter.Receipt.format_charge(%{id: "ch_abc", status: :succeeded, amount: 2500})
      "Receipt: ch_abc | Status: succeeded | Amount: $25.00"

  """
  @spec format_charge(map()) :: String.t()
  def format_charge(%{id: id, status: status, amount: amount}) do
    "Receipt: #{id} | Status: #{status} | Amount: #{format_amount(amount)}"
  end

  @doc """
  Generates a full receipt by charging and formatting in one step.

  ## Examples

      iex> {:ok, receipt} = PayAdapter.Receipt.charge_and_format("pm_visa", %{amount: 1000, currency: "usd"})
      iex> receipt =~ "Receipt:"
      true

  """
  @spec charge_and_format(String.t(), map()) :: {:ok, String.t()} | {:error, term()}
  def charge_and_format(payment_method, params) do
    case PayAdapter.charge(payment_method, params) do
      {:ok, result} -> {:ok, format_charge(result)}
      {:error, _} = error -> error
    end
  end

  @spec format_amount(non_neg_integer()) :: String.t()
  defp format_amount(cents) when is_integer(cents) do
    major = div(cents, 100)
    minor = rem(cents, 100) |> Integer.to_string() |> String.pad_leading(2, "0")
    "$#{major}.#{minor}"
  end
end
```

**Why this works:**

- `PayAdapter` has 4 public functions (`charge/2`, `refund/2`, `status/1`,
  `base_url/0`, `api_version/0`) and 10+ private functions. The public API
  is small and stable; the private implementation can be completely rewritten
  without affecting callers.
- Module attributes (`@base_url`, `@timeout_ms`, `@api_version`) are compile-time
  constants. They are inlined into the bytecode — there is no runtime lookup.
  Changing them requires recompilation.
- Private functions are grouped by responsibility (validation, request building,
  HTTP, response parsing) with comments marking each section. This makes the
  module navigable even at 150+ lines.
- `PayAdapter.Receipt` is a child module — it uses `alias PayAdapter` to reference
  the parent. In Elixir, the dot in module names is just a naming convention, not
  an inheritance hierarchy. `PayAdapter.Receipt` does not inherit anything from
  `PayAdapter`.

### Tests

```elixir
# test/pay_adapter_test.exs
defmodule PayAdapterTest do
  use ExUnit.Case, async: true

  doctest PayAdapter
  doctest PayAdapter.Receipt

  describe "charge/2" do
    test "charges successfully with valid params" do
      assert {:ok, result} = PayAdapter.charge("pm_visa_4242", %{amount: 2500, currency: "usd"})
      assert result.status == :succeeded
      assert result.amount == 2500
      assert String.starts_with?(result.id, "ch_")
    end

    test "rejects non-positive amount" do
      assert {:error, msg} = PayAdapter.charge("pm_visa", %{amount: -100, currency: "usd"})
      assert msg =~ "amount"
    end

    test "rejects invalid currency" do
      assert {:error, msg} = PayAdapter.charge("pm_visa", %{amount: 100, currency: "dollar"})
      assert msg =~ "currency"
    end

    test "accepts optional description" do
      assert {:ok, _} =
               PayAdapter.charge("pm_visa", %{amount: 100, currency: "usd", description: "Test"})
    end
  end

  describe "refund/2" do
    test "creates a refund" do
      assert {:ok, result} = PayAdapter.refund("ch_abc", %{amount: 1000, reason: "customer_request"})
      assert result.status == :pending
      assert String.starts_with?(result.id, "rf_")
    end

    test "works without amount (full refund)" do
      assert {:ok, result} = PayAdapter.refund("ch_abc")
      assert result.status == :pending
    end
  end

  describe "status/1" do
    test "returns charge status" do
      assert {:ok, result} = PayAdapter.status("ch_abc123")
      assert result.id == "ch_abc123"
      assert result.status == :succeeded
    end
  end

  describe "module attributes" do
    test "base_url is a known value" do
      assert PayAdapter.base_url() =~ "payments.example.com"
    end

    test "api_version follows date format" do
      assert PayAdapter.api_version() =~ ~r/\d{4}-\d{2}-\d{2}/
    end
  end

  describe "visibility" do
    test "private functions are not accessible" do
      assert_raise UndefinedFunctionError, fn ->
        PayAdapter.validate_charge_params(%{})
      end
    end

    test "private HTTP functions are not accessible" do
      assert_raise UndefinedFunctionError, fn ->
        PayAdapter.http_post("/test", %{})
      end
    end
  end

  describe "Receipt" do
    test "formats a charge result" do
      result = %{id: "ch_abc", status: :succeeded, amount: 2500}
      receipt = PayAdapter.Receipt.format_charge(result)
      assert receipt =~ "ch_abc"
      assert receipt =~ "succeeded"
      assert receipt =~ "$25.00"
    end

    test "charge_and_format combines charge and receipt" do
      assert {:ok, receipt} =
               PayAdapter.Receipt.charge_and_format("pm_visa", %{amount: 1000, currency: "usd"})

      assert receipt =~ "Receipt:"
      assert receipt =~ "$10.00"
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

---

## `alias`, `import`, `use`, and `require`

```elixir
# alias — shortens module names
alias PayAdapter.Receipt
Receipt.format_charge(result)  # instead of PayAdapter.Receipt.format_charge(result)

# import — brings functions into current scope (use sparingly)
import Enum, only: [map: 2, filter: 2]
map(list, &fun/1)  # instead of Enum.map(list, &fun/1)

# require — enables macros from another module
require Logger
Logger.info("message")  # Logger.info is a macro, needs require

# use — calls __using__ macro, injects code at compile time
use GenServer  # expands to: require GenServer; @behaviour GenServer; ...
```

Rules of thumb:
- `alias`: always fine, improves readability
- `import`: use sparingly, only `only:` specific functions
- `require`: only when using macros
- `use`: only when a library requires it (GenServer, Plug, Ecto.Schema)

---

## Common production mistakes

**1. Making everything public "for testing"**
If you need to test private logic, extract it into a separate module with public
functions and test that module directly. Do not make functions public just for
test access.

**2. Using module attributes for runtime configuration**
`@base_url "https://..."` is a compile-time constant. If you need runtime
configuration (environment variables), use `Application.get_env/3` or
`config/runtime.exs`.

**3. Circular module dependencies**
If module A calls module B and module B calls module A, you have a circular
dependency. This causes compilation order issues. Extract shared logic into
a third module.

**4. God modules with 50+ public functions**
If a module has too many public functions, it does too many things. Split it
into focused modules with clear responsibilities.

**5. Importing entire modules**
`import Enum` brings 70+ functions into scope, polluting the namespace.
Always use `import Enum, only: [map: 2]` to import only what you need.

---

## Resources

- [Modules — Elixir Getting Started](https://elixir-lang.org/getting-started/modules-and-functions.html)
- [alias, require, import — Elixir Getting Started](https://elixir-lang.org/getting-started/alias-require-and-import.html)
- [Module attributes — HexDocs](https://hexdocs.pm/elixir/Module.html)
- [Kernel.defp — HexDocs](https://hexdocs.pm/elixir/Kernel.html#defp/2)
