# Structs with @enforce_keys for Required Fields

**Project**: `user_signup` — a struct-based signup record with required and optional fields

---

## Project structure

```
user_signup/
├── lib/
│   └── user_signup.ex
├── script/
│   └── main.exs
├── test/
│   └── user_signup_test.exs
└── mix.exs
```

---

## The business problem

Your API receives user signup payloads. Some fields are **mandatory** (`email`, `password_hash`)
and some are **optional** (`referral_code`, `newsletter_opt_in`). A plain map lets any
key be missing without warning — bugs slip through to production where a `nil` email
crashes the notification worker hours later.

`defstruct` alone does not help: every field defaults to `nil`. You need `@enforce_keys`
to fail fast at struct creation time if a required field is missing.

---

## Core concepts

### `defstruct` with defaults

`defstruct` defines a compile-time struct. Every field becomes a key with a default value
(usually `nil`). The struct is really a map tagged with `__struct__: ModuleName`.

### `@enforce_keys`

`@enforce_keys` lists fields that MUST be provided at struct creation. If omitted, the
compiler raises an `ArgumentError` at the call site. This runs at the `%UserSignup{...}`
literal — not at pattern match, not at function boundaries. It is a constructor guard.

### Derived `Access` behaviour

Structs do NOT implement the `Access` behaviour by default — `user[:email]` fails.
You can derive it with `@derive {Access, ...}` or use `Map.get/2`, `get_in/2`, pattern
matching. For public structs, deriving `Access` makes the API friendlier; for internal
structs, the lack of `Access` prevents accidental misuse.

---

## Why @enforce_keys and not runtime validation

A runtime validator (`validate(struct)`) catches missing fields only when the function is called — potentially far from the construction site, in a different module, after the bad struct has traveled through several layers. `@enforce_keys` fails at the literal `%UserSignup{...}` expression, so the stack trace points to the exact line that created the invalid state. Runtime checks still have a role for cross-field invariants (e.g. "end_date must be after start_date"), but presence checks belong to the compiler.

Ecto changesets are the right tool for external input with accumulated errors, format rules, and uniqueness constraints — but they are heavy for internal domain records and add a dependency you may not need.

---

## Design decisions

**Option A — plain `defstruct` + runtime `validate/1` function**
- Pros: no compile-time rigidity; easy to build partial structs in tests.
- Cons: missing fields only discovered when `validate/1` is called; constructors scattered; stack traces point to the validator, not to the buggy caller.

**Option B — `@enforce_keys` + `from_params/1` safe constructor** (chosen)
- Pros: missing fields fail at compile site with precise location; safe constructor returns tagged tuple for I/O boundaries; internal code uses the struct literal directly and gets instant feedback.
- Cons: cannot build incomplete structs for tests without a helper; refactoring required fields is a breaking change for every call site.

Chose **B** because the domain invariant (`email` and `password_hash` must exist) is worth a breaking change when it shifts — call-site errors are cheap; a `nil` email reaching the notification worker is not.

---

## Implementation

### `mix.exs`
```elixir
defmodule UserSignup.MixProject do
  use Mix.Project

  def project do
    [
      app: :user_signup,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Create the smallest Mix library so the struct and its invariants live in a single, reviewable compilation unit.

```bash
mix new user_signup
cd user_signup
```

### `lib/user_signup.ex`

**Objective**: Use `@enforce_keys` to make missing mandatory fields fail at struct creation rather than hours later inside a downstream worker.

```elixir
defmodule UserSignup do
  @moduledoc """
  A signup record with required and optional fields.

  Required: `:email`, `:password_hash`
  Optional: `:referral_code` (nil), `:newsletter_opt_in` (false)
  """

  # @enforce_keys is checked at struct construction time.
  # If a caller writes `%UserSignup{}` without these keys,
  # the compiler raises an ArgumentError — we fail fast, not at runtime.
  @enforce_keys [:email, :password_hash]

  # Order matters for documentation but not for runtime.
  # Optional fields MUST have a default — otherwise they behave as required.
  defstruct [
    :email,
    :password_hash,
    referral_code: nil,
    newsletter_opt_in: false
  ]

  @type t :: %__MODULE__{
          email: String.t(),
          password_hash: String.t(),
          referral_code: String.t() | nil,
          newsletter_opt_in: boolean()
        }

  @doc """
  Builds a signup from a plain map (e.g. a decoded JSON body).

  Returns `{:ok, struct}` or `{:error, {:missing, [atom]}}` — we never let
  a missing required field reach the caller as an exception because this is
  I/O boundary code, not internal logic.
  """
  @spec from_params(map()) :: {:ok, t()} | {:error, {:missing, [atom()]}}
  def from_params(params) when is_map(params) do
    atom_params = for {k, v} <- params, into: %{}, do: {to_existing_atom(k), v}

    required = [:email, :password_hash]
    missing = Enum.reject(required, &Map.has_key?(atom_params, &1))

    case missing do
      [] -> {:ok, struct!(__MODULE__, atom_params)}
      keys -> {:error, {:missing, keys}}
    end
  end

  # We use `to_existing_atom` because unbounded `String.to_atom`
  # on untrusted input leaks atoms — the atom table is not GC'd.
  defp to_existing_atom(k) when is_atom(k), do: k
  defp to_existing_atom(k) when is_binary(k), do: String.to_existing_atom(k)
end
```

### Step 3: `test/user_signup_test.exs`

**Objective**: Lock in the `ArgumentError` contract for missing required keys so no future refactor can silently weaken the enforcement.

```elixir
defmodule UserSignupTest do
  use ExUnit.Case, async: true
  doctest UserSignup

  describe "struct creation" do
    test "succeeds with all required keys" do
      s = %UserSignup{email: "a@b.com", password_hash: "x"}
      assert s.email == "a@b.com"
      # Optional fields fall back to their defaults.
      assert s.newsletter_opt_in == false
      assert s.referral_code == nil
    end

    test "raises when a required key is missing" do
      # @enforce_keys turns this into a compile-time-style ArgumentError.
      assert_raise ArgumentError, fn ->
        # We wrap in Code.eval_string because `@enforce_keys` is checked
        # at the struct literal expansion — we need a runtime eval to test it.
        Code.eval_string("%UserSignup{email: \"a@b.com\"}")
      end
    end
  end

  describe "from_params/1" do
    test "accepts string keys and returns a struct" do
      params = %{"email" => "a@b.com", "password_hash" => "x"}
      assert {:ok, %UserSignup{email: "a@b.com"}} = UserSignup.from_params(params)
    end

    test "returns :error with the list of missing required keys" do
      assert {:error, {:missing, [:password_hash]}} =
               UserSignup.from_params(%{"email" => "a@b.com"})
    end
  end
end
```

### Step 4: Run the tests

**Objective**: Run the suite to confirm the struct enforces its contract at compile and construction time, not only in docs.

```bash
mix test
```

All tests must pass.

### Why this works

`@enforce_keys` hooks into the struct literal expansion: any `%UserSignup{...}` that omits a required key raises at the expression site, not at an opaque function deep in the call stack. `from_params/1` wraps that behaviour for untrusted input — it atomizes string keys safely with `String.to_existing_atom/1`, collects missing required keys into a single tagged-tuple error, and never lets the compile-time exception escape the I/O boundary. Internal code stays explicit with struct literals; external code stays safe with the constructor.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== UserSignup: demo ===\n")

    result_1 = Mix.env()
    IO.puts("Demo 1: #{inspect(result_1)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    params = %{"email" => "a@b.com", "password_hash" => "x"}

    {enforce_us, _} =
      :timer.tc(fn ->
        Enum.each(1..100_000, fn _ -> UserSignup.from_params(params) end)
      end)

    IO.puts("from_params x100k: #{enforce_us} µs (#{enforce_us / 100_000} µs/call)")
  end
end

Bench.run()
```

Target: under 5 µs per `from_params/1` call on modern hardware — most of the cost is the atom conversion and map reshape, not the `@enforce_keys` check (which is effectively free at runtime because it runs at the struct literal expansion).

---

## Trade-offs

| Approach | When to use |
|----------|-------------|
| Plain `defstruct` (no enforce) | Value objects where every field is optional (e.g. configs with sensible defaults) |
| `@enforce_keys` | Domain records where a missing field is a bug, not a valid state |
| `Ecto.Schema` + `changeset` | External input with validation rules beyond presence (format, length, uniqueness) |

`@enforce_keys` is for **internal invariants**. For user-supplied data, pair it with
`from_params/1`-style constructors that return tagged tuples — do NOT let the exception
propagate across an I/O boundary.

---

## Common production mistakes

**1. Putting every field in `@enforce_keys`**
If you list every field, users cannot build the struct incrementally (e.g. in test fixtures
or builder functions). Reserve `@enforce_keys` for fields whose absence is a true invariant
violation.

**2. Using `String.to_atom/1` on untrusted keys**
Atoms are not garbage-collected. An attacker sending random keys leaks atoms until the
node dies. Always use `String.to_existing_atom/1` at trust boundaries.

**3. Assuming `%{} = struct` works**
Structs match `%{}` (they ARE maps), but `%UserSignup{} = %{email: "x"}` fails — the
`__struct__` key is missing. Pattern-match the module when you need struct-ness.

**4. Missing `@type t`**
Without `@type t`, Dialyzer sees the struct as `%{optional(atom) => any}` and loses all
type information. Add `@type t` even for trivial structs.

---

## When NOT to use

- **Short-lived data between two functions in the same module**: a plain map is lighter.
- **Heterogeneous collections**: if the shape varies, use a tagged tuple or a sum type, not a struct with half the fields nil.
- **Wire formats** (JSON, protobuf): decode into a struct at the edge, but do not define a struct that mirrors a protocol 1:1 — that couples your domain to the wire format.

---

## Reflection

1. If a new required field (`country_code`) must be added to `UserSignup`, every existing call site that uses the struct literal breaks at compile time. Is that a feature or a defect? How would your answer change if the struct is used in 200 places across 5 applications?
2. `from_params/1` returns `{:error, {:missing, [...]}}` with a flat list of missing keys. Would you change the shape if the caller needs to render field-level errors in a signup form, or keep the boundary minimal and let the presentation layer translate?

---

```elixir
defmodule UserSignup do
  @moduledoc """
  A signup record with required and optional fields.

  Required: `:email`, `:password_hash`
  Optional: `:referral_code` (nil), `:newsletter_opt_in` (false)
  """

  # @enforce_keys is checked at struct construction time.
  # If a caller writes `%UserSignup{}` without these keys,
  # the compiler raises an ArgumentError — we fail fast, not at runtime.
  @enforce_keys [:email, :password_hash]

  # Order matters for documentation but not for runtime.
  # Optional fields MUST have a default — otherwise they behave as required.
  defstruct [
    :email,
    :password_hash,
    referral_code: nil,
    newsletter_opt_in: false
  ]

  @type t :: %__MODULE__{
          email: String.t(),
          password_hash: String.t(),
          referral_code: String.t() | nil,
          newsletter_opt_in: boolean()
        }

  @doc """
  Builds a signup from a plain map (e.g. a decoded JSON body).

  Returns `{:ok, struct}` or `{:error, {:missing, [atom]}}` — we never let
  a missing required field reach the caller as an exception because this is
  I/O boundary code, not internal logic.
  """
  @spec from_params(map()) :: {:ok, t()} | {:error, {:missing, [atom()]}}
  def from_params(params) when is_map(params) do
    atom_params = for {k, v} <- params, into: %{}, do: {to_existing_atom(k), v}

    required = [:email, :password_hash]
    missing = Enum.reject(required, &Map.has_key?(atom_params, &1))

    case missing do
      [] -> {:ok, struct!(__MODULE__, atom_params)}
      keys -> {:error, {:missing, keys}}
    end
  end

  # We use `to_existing_atom` because unbounded `String.to_atom`
  # on untrusted input leaks atoms — the atom table is not GC'd.
  defp to_existing_atom(k) when is_atom(k), do: k
  defp to_existing_atom(k) when is_binary(k), do: String.to_existing_atom(k)
end
```

## Resources

- [Elixir docs — `defstruct`](https://hexdocs.pm/elixir/Kernel.html#defstruct/1)
- [Elixir docs — `@enforce_keys`](https://hexdocs.pm/elixir/Kernel.html#defstruct/1-enforcing-keys)
- [Saša Jurić — "Towards Maintainable Elixir"](https://medium.com/very-big-things/towards-maintainable-elixir-the-core-and-the-interface-c267f0da43)

---

## Why Structs with @enforce_keys for Required Fields matters

Mastering **Structs with @enforce_keys for Required Fields** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/user_signup_test.exs`

```elixir
defmodule UserSignupTest do
  use ExUnit.Case, async: true

  doctest UserSignup

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert UserSignup.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. `@enforce_keys` Requires Fields at Creation

Enforced keys must be provided when creating a struct, even if they have defaults. This prevents silent bugs where required fields are omitted.

### 2. Enforced Keys Are Compile-Time Constraints

The constraint is only checked in `%User{}` literal construction. If you manually create a map and convert it with `struct(User, map)`, no check happens.

### 3. Use Changesets for Validation in Production

For real applications, `@enforce_keys` is not enough. Use `Ecto.Changeset` or build your own validation layer to handle type checking, format validation, and detailed error messages.

---
