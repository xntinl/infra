# Custom Exception Modules with defexception

**Difficulty**: ★☆☆☆☆
**Time**: 1–1.5 hours
**Project**: `domain_errors` — a library of domain-specific exceptions for a payments module

---

## Project structure

```
domain_errors/
├── lib/
│   └── domain_errors.ex
├── test/
│   └── domain_errors_test.exs
└── mix.exs
```

---

## The business problem

`raise "invalid email"` works but is useless for callers. They cannot pattern-match on a
string. Logs show opaque `RuntimeError`s. Observability tools cannot aggregate by
exception type.

Custom exceptions give each error class a **named struct** with typed fields. Callers
write `rescue e in InvalidEmail` and get a structured value to inspect.

---

## Core concepts

### `defexception`

Defines a struct tagged as an exception. It auto-implements `Exception` behaviour:
`exception/1` (build from args) and `message/1` (human message). You can override both.

### `message/1` callback

Returns a human-readable string. Called by `Exception.message/1`, Logger, and IEx.
Good messages include **enough context to debug without the stack trace**.

### `rescue e in ModuleName`

Pattern-matches the rescued exception by its struct tag. You can list multiple:
`rescue e in [InvalidEmail, InvalidAmount]`.

### `@enforce_keys` for exceptions

Same as ordinary structs (exercise 60). Forcing key presence at raise time means no
`%InvalidEmail{address: nil}` silently loses context.

---

## Implementation

### Step 1: Create the project

```bash
mix new domain_errors
cd domain_errors
```

### Step 2: `lib/domain_errors.ex`

```elixir
defmodule DomainErrors do
  @moduledoc """
  Domain-specific exceptions for a payments module.

  These exceptions represent PROGRAMMER errors (invalid input that should have
  been validated upstream) and INFRASTRUCTURE errors that a human operator must see.
  For expected user-facing validation errors, return {:error, reason} tuples —
  do not raise.
  """

  defmodule InvalidEmail do
    @moduledoc "Raised when an email address fails the module's format check."

    # defexception defines the struct and implements the Exception behaviour.
    # `:address` has no default — forcing callers to supply it keeps error
    # context intact. We do NOT set a default message here; we build it dynamically.
    defexception [:address]

    # We override message/1 to include the offending value.
    # Without this, Exception.message/1 would say "" which is useless.
    @impl true
    def message(%__MODULE__{address: address}) do
      "invalid email address: #{inspect(address)}"
    end
  end

  defmodule InvalidAmount do
    @moduledoc "Raised when a monetary amount is non-positive or not an integer (cents)."

    defexception [:amount, :currency]

    @impl true
    def message(%__MODULE__{amount: amount, currency: currency}) do
      "invalid amount #{inspect(amount)} for currency #{inspect(currency)} " <>
        "(must be a positive integer in minor units)"
    end
  end

  defmodule PaymentDeclined do
    @moduledoc "Raised when an upstream gateway declines a charge."

    # We give :reason a default so callers can raise with just a code.
    # We do NOT default :transaction_id — a nil tx_id is useless for debugging.
    defexception [:transaction_id, reason: :unknown]

    @impl true
    def message(%__MODULE__{transaction_id: tx, reason: reason}) do
      "payment declined (tx=#{inspect(tx)}, reason=#{inspect(reason)})"
    end
  end

  @doc """
  Validates an email and raises `InvalidEmail` with the offending value on failure.
  Use this at trust boundaries AFTER initial `{:error, _}` validation — it is meant
  to assert an invariant, not to report expected failure.
  """
  @spec ensure_email!(String.t()) :: :ok
  def ensure_email!(address) when is_binary(address) do
    # Deliberately simple — we're demonstrating exceptions, not email parsing.
    if String.contains?(address, "@") and String.length(address) > 3 do
      :ok
    else
      raise InvalidEmail, address: address
    end
  end

  def ensure_email!(other), do: raise(InvalidEmail, address: other)

  @spec ensure_amount!(integer(), String.t()) :: :ok
  def ensure_amount!(amount, currency)
      when is_integer(amount) and amount > 0 and is_binary(currency) do
    :ok
  end

  def ensure_amount!(amount, currency) do
    raise InvalidAmount, amount: amount, currency: currency
  end
end
```

### Step 3: `test/domain_errors_test.exs`

```elixir
defmodule DomainErrorsTest do
  use ExUnit.Case, async: true

  alias DomainErrors.{InvalidEmail, InvalidAmount, PaymentDeclined}

  describe "InvalidEmail" do
    test "raise and rescue carries the offending address" do
      try do
        DomainErrors.ensure_email!("not-an-email")
      rescue
        e in InvalidEmail ->
          # Structured access — the reason we built a custom exception.
          assert e.address == "not-an-email"
          assert Exception.message(e) =~ "not-an-email"
      end
    end

    test "non-binary input still raises with the original term" do
      assert_raise InvalidEmail, fn -> DomainErrors.ensure_email!(nil) end
    end
  end

  describe "InvalidAmount" do
    test "negative amount" do
      assert_raise InvalidAmount, fn -> DomainErrors.ensure_amount!(-1, "USD") end
    end

    test "message includes amount and currency for observability" do
      try do
        DomainErrors.ensure_amount!(0, "EUR")
      rescue
        e in InvalidAmount -> assert Exception.message(e) =~ "EUR"
      end
    end
  end

  describe "PaymentDeclined" do
    test "defaults :reason to :unknown when only tx is given" do
      # Illustrates that default values on :reason let callers raise tersely.
      e = %PaymentDeclined{transaction_id: "tx_123"}
      assert e.reason == :unknown
      assert Exception.message(e) =~ "tx_123"
    end
  end

  test "rescue matches by module, letting callers handle each error class distinctly" do
    # This is why custom exceptions beat raw strings: selective rescue.
    result =
      try do
        DomainErrors.ensure_email!("bad")
      rescue
        InvalidAmount -> :wrong_one
        InvalidEmail -> :caught_email
      end

    assert result == :caught_email
  end
end
```

### Step 4: Run tests

```bash
mix test
```

---

## Trade-offs

| Pattern | When |
|---------|------|
| `raise "message"` (RuntimeError) | Throwaway scripts, REPL experiments |
| `raise ArgumentError, message: "..."` | Stdlib-style "you called this wrong" |
| Custom exception struct | Your own domain needs structured rescue |
| `{:error, reason}` tuple | Expected failure, not exceptional |
| `{:error, %MyError{}}` | Expected failure + structured context (best of both) |

---

## Common production mistakes

**1. Not overriding `message/1`**
Default `message/1` returns the empty string or struct inspect. Observability tools
show nothing useful. Always override `message/1` to include the interesting fields.

**2. Using custom exceptions for expected failures**
A form validation error is NOT exceptional — it happens thousands of times a day. Return
`{:error, changeset}` or `{:error, %InvalidEmail{...}}` as a value, do not raise.
Exceptions should be rare; raising in a hot loop is slow and noisy.

**3. Leaking implementation details in the message**
`message: "DB error: connection to db-prod-1.internal.example.com:5432 refused"` ends up
in user-facing error pages if an upper layer mishandles. Keep message content meaningful
but not sensitive.

**4. Forgetting that `raise ModuleName` (no args) requires a defaultable struct**
`raise InvalidEmail` calls `InvalidEmail.exception([])`. If `:address` is required,
this fails confusingly. Either provide defaults or always pass `raise ModuleName, key: v`.

**5. Defining one giant `DomainError` with a `:type` field**
This is "stringly typed" exceptions. You lose `rescue e in SpecificError`. Define
one module per error class — it is cheap.

---

## When NOT to use

- **User-facing validation errors** — return changesets or tagged tuples. Raising forces every call site to wrap in `try/rescue`.
- **Library-internal signaling** — a private `{:error, :not_found}` is simpler than a custom exception.
- **Crossing BEAM boundaries** — exceptions don't serialize meaningfully to JSON. Convert to a tagged tuple at the HTTP/queue edge.

---

## Resources

- [Elixir docs — `defexception`](https://hexdocs.pm/elixir/Kernel.html#defexception/1)
- [Elixir docs — Exception behaviour](https://hexdocs.pm/elixir/Exception.html)
- [Ecto.Query.CompileError source](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/query.ex) — example of a well-designed domain exception
