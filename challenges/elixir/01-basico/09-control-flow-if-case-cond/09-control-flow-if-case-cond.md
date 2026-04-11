# Control Flow: Building a Data Validation Library

**Project**: `validator` — a user registration validator using `with` chains, `case`, and `cond`

---

## Why `with` replaces nested case statements

In most languages, validating user input means nested `if/else` or sequential
`try/catch` blocks. In Elixir, the `with` expression provides flat, readable
chains of validations that short-circuit on the first failure.

The progression of control flow tools in Elixir:

- **`case`**: match one value against multiple patterns. Use for branching on a single input.
- **`cond`**: evaluate multiple conditions. Use when patterns are boolean expressions.
- **`with`**: chain multiple operations that each return `{:ok, value}` or `{:error, reason}`.
  Use when you need "do A, then B, then C, stop at first failure."
- **`if`/`unless`**: two-branch conditionals. Use sparingly — pattern matching is usually clearer.

The key insight: `with` is not a loop or a try/catch. It is a flat pipeline of
pattern matches where each step feeds into the next. If any step does not match,
`with` returns the non-matching value directly.

---

## The business problem

Build a validation library for user registration that:

1. Validates email format, password strength, age, and username
2. Uses `with` to chain validations (all must pass)
3. Uses `case` for single-field validation with multiple patterns
4. Uses `cond` for password strength classification
5. Returns all errors at once (not just the first)

---

## Implementation

### `lib/validator.ex`

```elixir
defmodule Validator do
  @moduledoc """
  Validates user registration data using idiomatic Elixir control flow.

  Demonstrates `with` for sequential validation chains,
  `case` for pattern-based branching, and `cond` for boolean conditions.
  """

  @type validation_result :: :ok | {:error, String.t()}
  @type field_errors :: %{atom() => [String.t()]}

  @doc """
  Validates all registration fields using a `with` chain.

  Stops at the first validation failure and returns the error.
  Use `validate_all/1` to collect all errors at once.

  ## Examples

      iex> Validator.validate(%{email: "user@example.com", password: "Str0ng!Pass", username: "alice", age: 25})
      :ok

      iex> Validator.validate(%{email: "bad", password: "Str0ng!Pass", username: "alice", age: 25})
      {:error, "email must contain exactly one @ character"}

  """
  @spec validate(map()) :: :ok | {:error, String.t()}
  def validate(params) when is_map(params) do
    with :ok <- validate_email(params[:email]),
         :ok <- validate_password(params[:password]),
         :ok <- validate_username(params[:username]),
         :ok <- validate_age(params[:age]) do
      :ok
    end
  end

  @doc """
  Validates all fields and collects ALL errors, not just the first.

  This is the production pattern: show the user every problem at once
  so they can fix everything in one round.

  ## Examples

      iex> Validator.validate_all(%{email: "bad", password: "short", username: "", age: -1})
      {:error, %{email: ["email must contain exactly one @ character"], password: ["password must be at least 8 characters"], username: ["username is required"], age: ["age must be between 13 and 120"]}}

      iex> Validator.validate_all(%{email: "a@b.com", password: "Str0ng!Pass", username: "alice", age: 25})
      :ok

  """
  @spec validate_all(map()) :: :ok | {:error, field_errors()}
  def validate_all(params) when is_map(params) do
    validations = [
      {:email, validate_email(params[:email])},
      {:password, validate_password(params[:password])},
      {:username, validate_username(params[:username])},
      {:age, validate_age(params[:age])}
    ]

    errors =
      validations
      |> Enum.filter(fn {_field, result} -> result != :ok end)
      |> Enum.map(fn {field, {:error, msg}} -> {field, [msg]} end)
      |> Map.new()

    case errors do
      empty when map_size(empty) == 0 -> :ok
      errors -> {:error, errors}
    end
  end

  @doc """
  Validates an email address using pattern matching and string functions.

  ## Examples

      iex> Validator.validate_email("user@example.com")
      :ok

      iex> Validator.validate_email(nil)
      {:error, "email is required"}

      iex> Validator.validate_email("no-at-sign")
      {:error, "email must contain exactly one @ character"}

  """
  @spec validate_email(term()) :: validation_result()
  def validate_email(nil), do: {:error, "email is required"}
  def validate_email(""), do: {:error, "email is required"}

  def validate_email(email) when is_binary(email) do
    case String.split(email, "@") do
      [local, domain] when byte_size(local) > 0 and byte_size(domain) > 2 ->
        if String.contains?(domain, ".") do
          :ok
        else
          {:error, "email domain must contain a dot"}
        end

      [_, _] ->
        {:error, "email local part or domain is too short"}

      _ ->
        {:error, "email must contain exactly one @ character"}
    end
  end

  def validate_email(_), do: {:error, "email must be a string"}

  @doc """
  Validates password strength using `cond` for multi-condition checks.

  ## Examples

      iex> Validator.validate_password("Str0ng!Pass")
      :ok

      iex> Validator.validate_password("short")
      {:error, "password must be at least 8 characters"}

  """
  @spec validate_password(term()) :: validation_result()
  def validate_password(nil), do: {:error, "password is required"}

  def validate_password(password) when is_binary(password) do
    cond do
      String.length(password) < 8 ->
        {:error, "password must be at least 8 characters"}

      not Regex.match?(~r/[A-Z]/, password) ->
        {:error, "password must contain at least one uppercase letter"}

      not Regex.match?(~r/[0-9]/, password) ->
        {:error, "password must contain at least one digit"}

      not Regex.match?(~r/[^a-zA-Z0-9]/, password) ->
        {:error, "password must contain at least one special character"}

      true ->
        :ok
    end
  end

  def validate_password(_), do: {:error, "password must be a string"}

  @doc """
  Classifies password strength using `cond`.

  ## Examples

      iex> Validator.password_strength("Str0ng!Password123")
      :strong

      iex> Validator.password_strength("Str0ng!P")
      :medium

      iex> Validator.password_strength("weak")
      :weak

  """
  @spec password_strength(String.t()) :: :weak | :medium | :strong
  def password_strength(password) when is_binary(password) do
    score = compute_strength_score(password)

    cond do
      score >= 4 -> :strong
      score >= 2 -> :medium
      true -> :weak
    end
  end

  @doc """
  Validates a username.

  ## Examples

      iex> Validator.validate_username("alice")
      :ok

      iex> Validator.validate_username("")
      {:error, "username is required"}

      iex> Validator.validate_username("ab")
      {:error, "username must be between 3 and 20 characters"}

  """
  @spec validate_username(term()) :: validation_result()
  def validate_username(nil), do: {:error, "username is required"}
  def validate_username(""), do: {:error, "username is required"}

  def validate_username(username) when is_binary(username) do
    len = String.length(username)

    cond do
      len < 3 or len > 20 ->
        {:error, "username must be between 3 and 20 characters"}

      not Regex.match?(~r/^[a-zA-Z0-9_]+$/, username) ->
        {:error, "username must contain only letters, numbers, and underscores"}

      true ->
        :ok
    end
  end

  def validate_username(_), do: {:error, "username must be a string"}

  @doc """
  Validates age using guards.

  ## Examples

      iex> Validator.validate_age(25)
      :ok

      iex> Validator.validate_age(10)
      {:error, "age must be between 13 and 120"}

      iex> Validator.validate_age(nil)
      {:error, "age is required"}

  """
  @spec validate_age(term()) :: validation_result()
  def validate_age(nil), do: {:error, "age is required"}

  def validate_age(age) when is_integer(age) and age >= 13 and age <= 120 do
    :ok
  end

  def validate_age(age) when is_integer(age) do
    {:error, "age must be between 13 and 120"}
  end

  def validate_age(_), do: {:error, "age must be an integer"}

  # --- Private helpers ---

  @spec compute_strength_score(String.t()) :: non_neg_integer()
  defp compute_strength_score(password) do
    checks = [
      String.length(password) >= 12,
      Regex.match?(~r/[A-Z]/, password),
      Regex.match?(~r/[a-z]/, password),
      Regex.match?(~r/[0-9]/, password),
      Regex.match?(~r/[^a-zA-Z0-9]/, password)
    ]

    Enum.count(checks, & &1)
  end
end
```

**Why this works:**

- `validate/1` uses `with` to chain four validations. Each must return `:ok` for
  the next to execute. If `validate_email/1` returns `{:error, msg}`, `with`
  short-circuits and returns that error immediately. No nesting, no temporary variables.
- `validate_all/1` runs all validations regardless of failures, collecting errors
  into a map. This is the production pattern — users want to see all problems at once.
- `validate_password/1` uses `cond` because the conditions are sequential checks on
  the same value. `cond` is the right tool when you have multiple boolean conditions
  that are not pattern-matchable.
- `validate_email/1` uses `case` with pattern matching on `String.split/2` results.
  This is cleaner than regex for structural validation.
- `validate_age/1` uses guards (`when is_integer(age) and age >= 13`) for range
  validation. Guards are compiled to efficient native checks.

### Tests

```elixir
# test/validator_test.exs
defmodule ValidatorTest do
  use ExUnit.Case, async: true

  doctest Validator

  describe "validate/1 (with chain)" do
    test "passes with valid data" do
      params = %{
        email: "user@example.com",
        password: "Str0ng!Pass",
        username: "alice",
        age: 25
      }

      assert :ok = Validator.validate(params)
    end

    test "stops at first failure" do
      params = %{email: nil, password: nil, username: nil, age: nil}
      assert {:error, "email is required"} = Validator.validate(params)
    end

    test "returns password error when email is valid" do
      params = %{email: "a@b.com", password: "short", username: "alice", age: 25}
      assert {:error, msg} = Validator.validate(params)
      assert msg =~ "password"
    end
  end

  describe "validate_all/1" do
    test "returns all errors at once" do
      params = %{email: "bad", password: "weak", username: "", age: -1}
      assert {:error, errors} = Validator.validate_all(params)
      assert Map.has_key?(errors, :email)
      assert Map.has_key?(errors, :password)
      assert Map.has_key?(errors, :username)
      assert Map.has_key?(errors, :age)
    end

    test "returns :ok when all valid" do
      params = %{
        email: "user@example.com",
        password: "Str0ng!Pass",
        username: "alice",
        age: 25
      }

      assert :ok = Validator.validate_all(params)
    end
  end

  describe "validate_email/1" do
    test "accepts valid email" do
      assert :ok = Validator.validate_email("user@example.com")
    end

    test "rejects nil" do
      assert {:error, _} = Validator.validate_email(nil)
    end

    test "rejects missing @" do
      assert {:error, msg} = Validator.validate_email("noatsign")
      assert msg =~ "@"
    end

    test "rejects domain without dot" do
      assert {:error, _} = Validator.validate_email("user@localhost")
    end
  end

  describe "validate_password/1" do
    test "accepts strong password" do
      assert :ok = Validator.validate_password("Str0ng!Pass")
    end

    test "rejects short password" do
      assert {:error, msg} = Validator.validate_password("Sh0rt!")
      assert msg =~ "8 characters"
    end

    test "rejects without uppercase" do
      assert {:error, msg} = Validator.validate_password("str0ng!pass")
      assert msg =~ "uppercase"
    end

    test "rejects without digit" do
      assert {:error, msg} = Validator.validate_password("StrongPass!")
      assert msg =~ "digit"
    end

    test "rejects without special character" do
      assert {:error, msg} = Validator.validate_password("Str0ngPass")
      assert msg =~ "special"
    end
  end

  describe "password_strength/1" do
    test "strong password scores high" do
      assert Validator.password_strength("Str0ng!Password123") == :strong
    end

    test "medium password" do
      assert Validator.password_strength("Str0ng!P") == :medium
    end

    test "weak password" do
      assert Validator.password_strength("weak") == :weak
    end
  end

  describe "validate_username/1" do
    test "accepts valid username" do
      assert :ok = Validator.validate_username("alice_123")
    end

    test "rejects too short" do
      assert {:error, _} = Validator.validate_username("ab")
    end

    test "rejects special characters" do
      assert {:error, _} = Validator.validate_username("user@name")
    end
  end

  describe "validate_age/1" do
    test "accepts valid age" do
      assert :ok = Validator.validate_age(25)
    end

    test "rejects under 13" do
      assert {:error, _} = Validator.validate_age(10)
    end

    test "rejects over 120" do
      assert {:error, _} = Validator.validate_age(150)
    end

    test "rejects non-integer" do
      assert {:error, _} = Validator.validate_age("25")
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

---

## `with` vs nested `case`

Without `with`, the same validation looks like this:

```elixir
# Nested case — the "pyramid of doom"
case validate_email(params[:email]) do
  :ok ->
    case validate_password(params[:password]) do
      :ok ->
        case validate_username(params[:username]) do
          :ok -> validate_age(params[:age])
          error -> error
        end
      error -> error
    end
  error -> error
end
```

With `with`:

```elixir
with :ok <- validate_email(params[:email]),
     :ok <- validate_password(params[:password]),
     :ok <- validate_username(params[:username]),
     :ok <- validate_age(params[:age]) do
  :ok
end
```

Same logic, flat structure, no repetition of error handling.

---

## Common production mistakes

**1. Using `if` when `case` or pattern matching is clearer**
```elixir
# Bad — testing a value you could pattern match
if result == :ok, do: ...

# Good
case result do
  :ok -> ...
  {:error, reason} -> ...
end
```

**2. Forgetting the `else` clause in `with`**
Without an `else` clause, `with` returns the first non-matching value as-is.
If your steps return different error shapes (`{:error, msg}`, `:error`, `nil`),
add `else` to normalize them.

**3. Using `cond` when `case` with patterns is available**
`cond` evaluates boolean expressions. If you are matching on the shape of a value,
`case` with patterns is more idiomatic and enables compiler checks.

**4. Putting side effects inside `with` clauses**
Each `<-` clause in `with` should be a pure function call. Putting IO or database
writes inside `with` makes error handling unpredictable — what do you do if step 3
fails but step 2 already wrote to the database?

---

## Resources

- [case, cond, and if — Elixir Getting Started](https://elixir-lang.org/getting-started/case-cond-and-if.html)
- [with — Kernel.SpecialForms](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1)
- [Guards — HexDocs](https://hexdocs.pm/elixir/patterns-and-guards.html#guards)
