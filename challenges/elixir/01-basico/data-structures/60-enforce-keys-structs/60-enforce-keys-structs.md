# Structs with @enforce_keys for Required Fields

**Difficulty**: ★☆☆☆☆
**Time**: 1–1.5 hours
**Project**: `user_signup` — a struct-based signup record with required and optional fields

---

## Project structure

```
user_signup/
├── lib/
│   └── user_signup.ex
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

## Implementation

### Step 1: Create the project

```bash
mix new user_signup
cd user_signup
```

### Step 2: `lib/user_signup.ex`

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

```elixir
defmodule UserSignupTest do
  use ExUnit.Case, async: true

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

```bash
mix test
```

All tests must pass.

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

## Resources

- [Elixir docs — `defstruct`](https://hexdocs.pm/elixir/Kernel.html#defstruct/1)
- [Elixir docs — `@enforce_keys`](https://hexdocs.pm/elixir/Kernel.html#defstruct/1-enforcing-keys)
- [Saša Jurić — "Towards Maintainable Elixir"](https://medium.com/very-big-things/towards-maintainable-elixir-the-core-and-the-interface-c267f0da43)
