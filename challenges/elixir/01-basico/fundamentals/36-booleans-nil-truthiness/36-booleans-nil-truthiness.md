# Booleans, Nil and Truthiness: Building a Feature Flag Evaluator

**Project**: `feature_flag_evaluator` — evaluates feature flags with defaults and fallbacks using truthy/falsy semantics

**Difficulty**: ★☆☆☆☆
**Estimated time**: 1-2 hours

---

## Project structure

```
feature_flag_evaluator/
├── lib/
│   └── feature_flag_evaluator.ex
├── test/
│   └── feature_flag_evaluator_test.exs
└── mix.exs
```

---

## Core concepts

Elixir has two distinct "truthiness" worlds:

1. **Strict boolean world** — `and`, `or`, `not` require actual `true` or `false`.
   They raise `ArgumentError` if you pass anything else.
2. **Truthy world** — `&&`, `||`, `!` accept ANY value. Only `false` and `nil`
   are falsy; everything else (including `0`, `""`, `[]`) is truthy.

For a senior developer coming from JavaScript or Python, the surprise is that `0`
and empty collections are truthy. This matters when you write `if config[:timeout]`
— a timeout of `0` will be evaluated as truthy (unlike Python, where `0` is falsy).

The second concept is `nil` as the "missing value". `nil` is actually the atom
`:nil`, and it is the ONLY falsy value besides `false`. This makes
`value || default` the canonical pattern for fallbacks.

---

## The business problem

A feature flag system needs to decide whether a feature is enabled for a given
user. Flags come from multiple sources with priority:

1. Per-user override (may be `nil` = not set)
2. Tenant-level setting (may be `nil`)
3. Global default (must always exist)

We must never treat `false` as "not set". A user explicitly disabling a feature
(`false`) must not fall back to the tenant default. Only `nil` means "missing".

---

## Implementation

### `lib/feature_flag_evaluator.ex`

```elixir
defmodule FeatureFlagEvaluator do
  @moduledoc """
  Evaluates feature flags using layered fallbacks.

  Priority: user override -> tenant setting -> global default.
  Only `nil` triggers fallback. An explicit `false` is respected.
  """

  @type flag_value :: boolean() | nil
  @type user_flags :: %{optional(atom()) => flag_value()}
  @type tenant_flags :: %{optional(atom()) => flag_value()}
  @type global_flags :: %{optional(atom()) => boolean()}

  @doc """
  Resolves a flag by walking the priority chain.

  Uses explicit nil checks — NOT `||` — because `||` would treat an
  explicit `false` as "missing" and fall through to the next layer.
  """
  @spec enabled?(atom(), user_flags(), tenant_flags(), global_flags()) :: boolean()
  def enabled?(flag, user, tenant, global)
      when is_atom(flag) and is_map(user) and is_map(tenant) and is_map(global) do
    cond do
      # `Map.get/2` returns nil when absent, which is the "not set" signal.
      # We use `is_boolean/1` to distinguish a real value from nil.
      is_boolean(Map.get(user, flag)) -> Map.fetch!(user, flag)
      is_boolean(Map.get(tenant, flag)) -> Map.fetch!(tenant, flag)
      true -> Map.get(global, flag, false)
    end
  end

  @doc """
  Naive implementation using `||` — shows the bug.

  Kept here for the test suite to demonstrate why strict nil checks matter:
  a user who disables a flag (`false`) ends up with the tenant value
  because `false || tenant_value` evaluates to `tenant_value`.
  """
  @spec enabled_naive?(atom(), user_flags(), tenant_flags(), global_flags()) :: boolean()
  def enabled_naive?(flag, user, tenant, global) do
    # Bug: `false || x` returns x, but `false` is a real decision, not absence.
    Map.get(user, flag) || Map.get(tenant, flag) || Map.get(global, flag, false)
  end

  @doc """
  Returns the full evaluation report: value + source layer.

  Useful for debugging why a flag resolved the way it did.
  """
  @spec evaluate(atom(), user_flags(), tenant_flags(), global_flags()) ::
          {boolean(), :user | :tenant | :global | :default}
  def evaluate(flag, user, tenant, global) do
    cond do
      is_boolean(Map.get(user, flag)) -> {Map.fetch!(user, flag), :user}
      is_boolean(Map.get(tenant, flag)) -> {Map.fetch!(tenant, flag), :tenant}
      is_boolean(Map.get(global, flag)) -> {Map.fetch!(global, flag), :global}
      true -> {false, :default}
    end
  end

  @doc """
  Demonstrates strict vs truthy operators side by side.

  `and`/`or`/`not` require booleans. Passing `nil` raises. This is what
  you want for domain logic where only true/false make sense.

  `&&`/`||`/`!` accept anything. Useful for guards and defaults, dangerous
  when `false` is a valid value.
  """
  @spec strict_all_enabled?([boolean()]) :: boolean()
  def strict_all_enabled?(flags) when is_list(flags) do
    # Raises ArgumentError if any element is not a boolean — fail fast.
    Enum.reduce(flags, true, fn flag, acc -> acc and flag end)
  end
end
```

### `test/feature_flag_evaluator_test.exs`

```elixir
defmodule FeatureFlagEvaluatorTest do
  use ExUnit.Case, async: true

  alias FeatureFlagEvaluator

  describe "enabled?/4 (correct version)" do
    test "user true overrides tenant false" do
      user = %{dark_mode: true}
      tenant = %{dark_mode: false}
      global = %{dark_mode: false}
      assert FeatureFlagEvaluator.enabled?(:dark_mode, user, tenant, global)
    end

    test "user false overrides tenant true" do
      user = %{dark_mode: false}
      tenant = %{dark_mode: true}
      global = %{dark_mode: true}
      refute FeatureFlagEvaluator.enabled?(:dark_mode, user, tenant, global)
    end

    test "missing user falls back to tenant" do
      assert FeatureFlagEvaluator.enabled?(:beta, %{}, %{beta: true}, %{beta: false})
    end

    test "missing user and tenant falls back to global" do
      assert FeatureFlagEvaluator.enabled?(:beta, %{}, %{}, %{beta: true})
    end

    test "missing everywhere returns false" do
      refute FeatureFlagEvaluator.enabled?(:unknown, %{}, %{}, %{})
    end

    test "explicit nil in user falls through" do
      # nil means "not set" — fall back to tenant.
      assert FeatureFlagEvaluator.enabled?(:x, %{x: nil}, %{x: true}, %{})
    end
  end

  describe "enabled_naive?/4 (buggy version)" do
    test "bug: user false is ignored when tenant is true" do
      user = %{dark_mode: false}
      tenant = %{dark_mode: true}
      global = %{}
      # The naive version returns true because `false || true` is true.
      assert FeatureFlagEvaluator.enabled_naive?(:dark_mode, user, tenant, global)
    end
  end

  describe "evaluate/4" do
    test "reports source layer" do
      assert {true, :user} =
               FeatureFlagEvaluator.evaluate(:x, %{x: true}, %{}, %{})

      assert {false, :tenant} =
               FeatureFlagEvaluator.evaluate(:x, %{}, %{x: false}, %{x: true})

      assert {true, :global} =
               FeatureFlagEvaluator.evaluate(:x, %{}, %{}, %{x: true})

      assert {false, :default} =
               FeatureFlagEvaluator.evaluate(:x, %{}, %{}, %{})
    end
  end

  describe "strict operators" do
    test "all booleans returns conjunction" do
      assert FeatureFlagEvaluator.strict_all_enabled?([true, true, true])
      refute FeatureFlagEvaluator.strict_all_enabled?([true, false, true])
    end

    test "raises if a non-boolean slips in" do
      # `and` rejects nil — this is the feature, not a bug.
      assert_raise ArgumentError, fn ->
        FeatureFlagEvaluator.strict_all_enabled?([true, nil])
      end
    end
  end

  describe "truthiness quick reference" do
    test "only false and nil are falsy" do
      assert !!0
      assert !!""
      assert !![]
      assert !!%{}
      refute !!false
      refute !!nil
    end
  end
end
```

### Run it

```bash
mix new feature_flag_evaluator
cd feature_flag_evaluator
# Copy the files above
mix test
```

---

## Trade-offs and production mistakes

**1. `||` is wrong for "has the user set this?"**
`Map.get(user, :flag) || default` treats `false` as missing. Use
`Map.has_key?/2` or match explicitly on `nil` when `false` is meaningful.

**2. `and`/`or` raise on non-booleans**
`nil and true` raises `ArgumentError`. In guards this is an advantage (fail
fast). In user-facing logic use `&&`/`||` or explicit conversion.

**3. `0` is truthy**
`if counter, do: :has_value, else: :none` returns `:has_value` when counter
is `0`. If you need "zero means absent", test explicitly: `if counter > 0`.

**4. `nil` and `false` are atoms**
`is_atom(nil)` returns `true`. Both live in the atom table but are special-cased.

## When NOT to use truthy operators

- Inside guards: only a subset works, and behavior differs. Use `and`/`or`/`not`.
- In domain code where `false` is a real value distinct from "missing" (this
  exercise's entire point).
- When the Boolean module (`Boolean.to_integer/1`, etc.) gives clearer intent.

---

## Resources

- [Boolean and nil — Getting Started](https://elixir-lang.org/getting-started/basic-types.html#booleans-and-nil)
- [Kernel.&&/2](https://hexdocs.pm/elixir/Kernel.html#&&/2)
- [Kernel.and/2](https://hexdocs.pm/elixir/Kernel.html#and/2)
- [Truthiness — Elixir School](https://elixirschool.com/en/lessons/basics/basics#truthiness-and-boolean-comparisons)
