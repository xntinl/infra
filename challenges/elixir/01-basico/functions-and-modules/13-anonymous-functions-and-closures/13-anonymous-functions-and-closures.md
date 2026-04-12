# Anonymous Functions and Closures: Building a Rules Engine

**Project**: `rules_engine` — a business rules engine where rules are first-class functions composed at runtime

---

## Why closures replace the Strategy pattern

In Java or Go, the Strategy pattern requires defining an interface, implementing
it in multiple classes, and injecting the strategy at construction time. In Elixir,
a function IS a strategy. You do not need interfaces, classes, or dependency injection
frameworks — just pass a function.

```elixir
# Java needs: interface, classes, constructor injection
# Elixir needs: a function

# Define a rule as a function
age_check = fn user -> user.age >= 18 end

# Compose rules
rules = [age_check, fn user -> user.verified end]

# Apply them
Enum.all?(rules, fn rule -> rule.(user) end)
```

Closures add another dimension: a function can capture variables from its creation
context. This means you can create parameterized rules at runtime without classes:

```elixir
# min_age is "closed over" — captured at creation time
def min_age_rule(min_age) do
  fn user -> user.age >= min_age end
end

# Creates different functions with different captured values
adult_rule = min_age_rule(18)
senior_rule = min_age_rule(65)
```

---

## The business problem

Build a rules engine for an insurance quoting system that:

1. Defines rules as anonymous functions (closures)
2. Composes rules using `and`, `or`, and `not` combinators
3. Creates parameterized rules using factory functions (closures)
4. Evaluates a set of rules against an applicant and returns a decision
5. Provides detailed explanations for which rules passed or failed

---

## Project structure

```
rules_engine/
├── lib/
│   └── rules_engine.ex
├── test/
│   └── rules_engine_test.exs
└── mix.exs
```

---

## Why closures and not behaviour modules

**Option A — one module per rule, all implementing a `Rule` behaviour**
- Pros: each rule is documented and testable in isolation; compile-time dispatch; plays well with hot code reload.
- Cons: defining a 5-line rule requires a new module and a new file; runtime composition (e.g. loading rules from config) becomes a metaprogramming exercise.

**Option B — rules as closures passed as values** (chosen)
- Pros: rules are created, composed, and stored at runtime; `min_age_rule(18)` and `min_age_rule(65)` are *different values*, not different modules; combinators (`and_rule/2`) are ordinary functions.
- Cons: rules are anonymous in stack traces (`#Function<...>`), so debugging requires naming conventions or tagged tuples.

→ Chose **B** because rules come from configuration, user input, or the database — not from the source tree. Closures match the data lifecycle.

---

## Design decisions

**Option A — rule returns boolean, combinators operate on booleans**
- Pros: simplest possible interface; easy to parallelise.
- Cons: a failed decision gives you no explanation; debugging a rejected applicant means re-running each rule by hand.

**Option B — rule returns `{:pass, name}` or `{:fail, name, reason}`, combinators carry the trace** (chosen)
- Pros: the engine can produce "rule X failed because …" without re-executing; auditability comes for free.
- Cons: combinators must short-circuit while preserving the trace, so `and_rule/2` is slightly more than `Enum.all?/2`.

→ Chose **B** because in insurance underwriting the *why* of a rejection is regulated artefact. Losing it to compress the interface is a false economy.

---

## Implementation

### `lib/rules_engine.ex`

```elixir
defmodule RulesEngine do
  @moduledoc """
  A business rules engine using anonymous functions and closures.

  Rules are functions that take a subject and return a boolean.
  Rule factories are functions that return rules (closures capturing config).
  Combinators compose rules into complex logic without inheritance.
  """

  @type subject :: map()
  @type rule :: (subject() -> boolean())
  @type named_rule :: {String.t(), rule()}

  # --- Rule Factories (closures) ---

  @doc """
  Creates a rule that checks if a field value meets a minimum.

  The returned function closes over `field` and `minimum`,
  capturing them at creation time.

  ## Examples

      iex> rule = RulesEngine.min_value(:age, 18)
      iex> rule.(%{age: 25})
      true
      iex> rule.(%{age: 16})
      false

  """
  @spec min_value(atom(), number()) :: rule()
  def min_value(field, minimum) do
    fn subject ->
      value = Map.get(subject, field, 0)
      is_number(value) and value >= minimum
    end
  end

  @doc """
  Creates a rule that checks if a field value does not exceed a maximum.

  ## Examples

      iex> rule = RulesEngine.max_value(:debt_ratio, 0.5)
      iex> rule.(%{debt_ratio: 0.3})
      true
      iex> rule.(%{debt_ratio: 0.8})
      false

  """
  @spec max_value(atom(), number()) :: rule()
  def max_value(field, maximum) do
    fn subject ->
      value = Map.get(subject, field, 0)
      is_number(value) and value <= maximum
    end
  end

  @doc """
  Creates a rule that checks if a field is in a set of allowed values.

  ## Examples

      iex> rule = RulesEngine.in_set(:state, MapSet.new(["CA", "NY", "TX"]))
      iex> rule.(%{state: "CA"})
      true
      iex> rule.(%{state: "FL"})
      false

  """
  @spec in_set(atom(), MapSet.t()) :: rule()
  def in_set(field, allowed_values) do
    fn subject ->
      MapSet.member?(allowed_values, Map.get(subject, field))
    end
  end

  @doc """
  Creates a rule that checks if a field matches a regex pattern.

  ## Examples

      iex> rule = RulesEngine.matches(:email, ~r/@.+\\./)
      iex> rule.(%{email: "user@example.com"})
      true
      iex> rule.(%{email: "invalid"})
      false

  """
  @spec matches(atom(), Regex.t()) :: rule()
  def matches(field, pattern) do
    fn subject ->
      value = Map.get(subject, field, "")
      is_binary(value) and Regex.match?(pattern, value)
    end
  end

  # --- Combinators ---

  @doc """
  Combines rules with AND logic — all must pass.

  ## Examples

      iex> rule = RulesEngine.all_of([
      ...>   RulesEngine.min_value(:age, 18),
      ...>   RulesEngine.max_value(:debt_ratio, 0.5)
      ...> ])
      iex> rule.(%{age: 25, debt_ratio: 0.3})
      true
      iex> rule.(%{age: 25, debt_ratio: 0.8})
      false

  """
  @spec all_of([rule()]) :: rule()
  def all_of(rules) when is_list(rules) do
    fn subject ->
      Enum.all?(rules, fn rule -> rule.(subject) end)
    end
  end

  @doc """
  Combines rules with OR logic — at least one must pass.

  ## Examples

      iex> rule = RulesEngine.any_of([
      ...>   RulesEngine.min_value(:age, 65),
      ...>   RulesEngine.min_value(:years_employed, 10)
      ...> ])
      iex> rule.(%{age: 70, years_employed: 2})
      true
      iex> rule.(%{age: 30, years_employed: 15})
      true
      iex> rule.(%{age: 30, years_employed: 2})
      false

  """
  @spec any_of([rule()]) :: rule()
  def any_of(rules) when is_list(rules) do
    fn subject ->
      Enum.any?(rules, fn rule -> rule.(subject) end)
    end
  end

  @doc """
  Negates a rule.

  ## Examples

      iex> not_minor = RulesEngine.negate(RulesEngine.max_value(:age, 17))
      iex> not_minor.(%{age: 25})
      true
      iex> not_minor.(%{age: 15})
      false

  """
  @spec negate(rule()) :: rule()
  def negate(rule) do
    fn subject -> not rule.(subject) end
  end

  # --- Evaluation with explanations ---

  @doc """
  Evaluates named rules and returns a detailed result.

  Each named rule is a {name, rule_function} tuple. The result
  includes which rules passed, which failed, and the overall decision.

  ## Examples

      iex> rules = [
      ...>   {"minimum age", RulesEngine.min_value(:age, 18)},
      ...>   {"low debt", RulesEngine.max_value(:debt_ratio, 0.5)}
      ...> ]
      iex> result = RulesEngine.evaluate(rules, %{age: 25, debt_ratio: 0.3})
      iex> result.approved
      true
      iex> length(result.passed)
      2

  """
  @spec evaluate([named_rule()], subject()) :: map()
  def evaluate(named_rules, subject) when is_list(named_rules) and is_map(subject) do
    results =
      Enum.map(named_rules, fn {name, rule} ->
        {name, rule.(subject)}
      end)

    passed = for {name, true} <- results, do: name
    failed = for {name, false} <- results, do: name

    %{
      approved: failed == [],
      passed: passed,
      failed: failed,
      total_rules: length(named_rules)
    }
  end

  @doc """
  Builds a rule set from a declarative configuration.

  Demonstrates how closures enable configuration-driven behavior:
  the rules are created at startup from a data structure,
  not hardcoded as module functions.

  ## Examples

      iex> config = [
      ...>   %{name: "adult", type: :min, field: :age, value: 18},
      ...>   %{name: "low debt", type: :max, field: :debt_ratio, value: 0.5}
      ...> ]
      iex> rules = RulesEngine.from_config(config)
      iex> result = RulesEngine.evaluate(rules, %{age: 25, debt_ratio: 0.3})
      iex> result.approved
      true

  """
  @spec from_config([map()]) :: [named_rule()]
  def from_config(config) when is_list(config) do
    Enum.map(config, fn rule_def ->
      {rule_def.name, build_rule(rule_def)}
    end)
  end

  @spec build_rule(map()) :: rule()
  defp build_rule(%{type: :min, field: field, value: value}) do
    min_value(field, value)
  end

  defp build_rule(%{type: :max, field: field, value: value}) do
    max_value(field, value)
  end

  defp build_rule(%{type: :in, field: field, value: values}) do
    in_set(field, MapSet.new(values))
  end

  defp build_rule(%{type: :matches, field: field, value: pattern}) do
    matches(field, Regex.compile!(pattern))
  end
end
```

### Why this works

- Rule factories (`min_value/2`, `max_value/2`, etc.) return anonymous functions
  that close over their parameters. Each call creates a new function with different
  captured values. This is the Strategy pattern without classes.
- Combinators (`all_of/1`, `any_of/1`, `negate/1`) take rules and return new rules.
  This is function composition — building complex behavior from simple pieces.
- `evaluate/2` takes named rules (tuples of name + function) and applies each to
  the subject. The comprehension `for {name, true} <- results` filters only passing
  rules using pattern matching inside the `for`.
- `from_config/1` builds rules from data at runtime. The rules are closures, so
  they carry their configuration internally. No global state needed.

### Tests

```elixir
# test/rules_engine_test.exs
defmodule RulesEngineTest do
  use ExUnit.Case, async: true

  doctest RulesEngine

  @applicant %{
    age: 35,
    income: 75_000,
    debt_ratio: 0.3,
    years_employed: 5,
    state: "CA",
    email: "alice@example.com"
  }

  describe "rule factories" do
    test "min_value creates a threshold rule" do
      rule = RulesEngine.min_value(:age, 18)
      assert rule.(%{age: 25})
      refute rule.(%{age: 16})
    end

    test "max_value creates a ceiling rule" do
      rule = RulesEngine.max_value(:debt_ratio, 0.5)
      assert rule.(%{debt_ratio: 0.3})
      refute rule.(%{debt_ratio: 0.8})
    end

    test "in_set checks membership" do
      rule = RulesEngine.in_set(:state, MapSet.new(["CA", "NY"]))
      assert rule.(%{state: "CA"})
      refute rule.(%{state: "FL"})
    end

    test "matches checks regex" do
      rule = RulesEngine.matches(:email, ~r/@.+\./)
      assert rule.(%{email: "a@b.com"})
      refute rule.(%{email: "invalid"})
    end

    test "handles missing fields gracefully" do
      rule = RulesEngine.min_value(:age, 18)
      refute rule.(%{})
    end
  end

  describe "combinators" do
    test "all_of requires all rules to pass" do
      rule = RulesEngine.all_of([
        RulesEngine.min_value(:age, 18),
        RulesEngine.max_value(:debt_ratio, 0.5)
      ])

      assert rule.(@applicant)
      refute rule.(%{age: 25, debt_ratio: 0.8})
    end

    test "any_of requires at least one rule to pass" do
      rule = RulesEngine.any_of([
        RulesEngine.min_value(:age, 65),
        RulesEngine.min_value(:years_employed, 10)
      ])

      refute rule.(@applicant)
      assert rule.(%{age: 70, years_employed: 1})
    end

    test "negate inverts a rule" do
      rule = RulesEngine.negate(RulesEngine.max_value(:age, 17))
      assert rule.(@applicant)
      refute rule.(%{age: 15})
    end

    test "combinators compose deeply" do
      rule = RulesEngine.all_of([
        RulesEngine.min_value(:age, 18),
        RulesEngine.any_of([
          RulesEngine.min_value(:income, 50_000),
          RulesEngine.min_value(:years_employed, 10)
        ]),
        RulesEngine.negate(RulesEngine.in_set(:state, MapSet.new(["XX"])))
      ])

      assert rule.(@applicant)
    end
  end

  describe "evaluate/2" do
    test "returns detailed results" do
      rules = [
        {"minimum age", RulesEngine.min_value(:age, 18)},
        {"low debt", RulesEngine.max_value(:debt_ratio, 0.5)},
        {"valid state", RulesEngine.in_set(:state, MapSet.new(["CA", "NY"]))}
      ]

      result = RulesEngine.evaluate(rules, @applicant)
      assert result.approved == true
      assert length(result.passed) == 3
      assert result.failed == []
      assert result.total_rules == 3
    end

    test "reports failures" do
      rules = [
        {"minimum age", RulesEngine.min_value(:age, 18)},
        {"high income", RulesEngine.min_value(:income, 100_000)}
      ]

      result = RulesEngine.evaluate(rules, @applicant)
      assert result.approved == false
      assert "high income" in result.failed
      assert "minimum age" in result.passed
    end
  end

  describe "from_config/1" do
    test "builds rules from declarative config" do
      config = [
        %{name: "adult", type: :min, field: :age, value: 18},
        %{name: "low debt", type: :max, field: :debt_ratio, value: 0.5},
        %{name: "valid state", type: :in, field: :state, value: ["CA", "NY"]}
      ]

      rules = RulesEngine.from_config(config)
      result = RulesEngine.evaluate(rules, @applicant)
      assert result.approved == true
    end

    test "config-based rules work the same as manual rules" do
      config_rules =
        RulesEngine.from_config([
          %{name: "age check", type: :min, field: :age, value: 18}
        ])

      manual_rules = [{"age check", RulesEngine.min_value(:age, 18)}]

      subject = %{age: 25}
      assert RulesEngine.evaluate(config_rules, subject) ==
               RulesEngine.evaluate(manual_rules, subject)
    end
  end

  describe "closure behavior" do
    test "each factory call creates an independent function" do
      rule_18 = RulesEngine.min_value(:age, 18)
      rule_65 = RulesEngine.min_value(:age, 65)

      subject = %{age: 30}
      assert rule_18.(subject)
      refute rule_65.(subject)
    end

    test "closures capture values, not references" do
      rules =
        Enum.map([18, 21, 65], fn min ->
          RulesEngine.min_value(:age, min)
        end)

      subject = %{age: 25}
      results = Enum.map(rules, fn rule -> rule.(subject) end)
      assert results == [true, true, false]
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

---

## Anonymous functions vs named functions

```elixir
# Anonymous — created with fn/end, called with dot syntax
add = fn a, b -> a + b end
add.(1, 2)  # => 3

# Named — defined with def/defp inside a module
defmodule Math do
  def add(a, b), do: a + b
end
Math.add(1, 2)  # => 3

# Key differences:
# 1. Anonymous functions use .() to call
# 2. Anonymous functions can close over variables
# 3. Named functions can have multiple clauses with pattern matching
# 4. Named functions can have guards
# 5. Named functions are compile-time; anonymous are runtime
```

Use anonymous functions for: callbacks, closures, short transformations passed
to Enum functions. Use named functions for: reusable logic, pattern-matching
dispatching, public APIs.

---

## Benchmark

Measure the cost of evaluating a composed rule so you can decide whether to cache decisions or re-run on every quote.

```elixir
rule =
  RulesEngine.and_rule([
    RulesEngine.min_age_rule(18),
    RulesEngine.max_age_rule(70),
    fn user -> user.verified end
  ])

applicant = %{age: 32, verified: true}

{us, _} = :timer.tc(fn ->
  for _ <- 1..1_000_000, do: rule.(applicant)
end)

IO.puts("per evaluation: #{us / 1_000_000} µs")
```

Target esperado: <1 µs per evaluation for a 3-rule composition. If you approach 10 µs, the cost is inside the predicates (e.g. database calls hiding in a rule), not in closure dispatch.

---

## Reflection

- A product manager wants rules to be editable from an admin UI at runtime. Do you keep closures (and evaluate serialised expressions with something like `Abacus`), switch to a behaviour with recompilation, or store rule ASTs and interpret them? What's the blast radius of each choice if a bad rule ships to production?
- Your trace format is `{:fail, rule_name, reason}`. A regulator asks for the exact *input value* that caused each failure, for every rejected applicant. How do you retrofit that without duplicating every predicate?

---

## Common production mistakes

**1. Forgetting the dot for anonymous function calls**
`fun(arg)` calls a named function. `fun.(arg)` calls an anonymous function.
This is a frequent source of `UndefinedFunctionError` for newcomers.

**2. Closures capture the value, not a reference**
```elixir
x = 1
f = fn -> x end
x = 2
f.()  # => 1, not 2 — the closure captured the VALUE 1
```

**3. Capturing the wrong arity**
`&String.trim/1` captures the 1-arity `trim`. If you need `&String.trim/2`
(with a specific character), capture the right arity.

**4. Over-using anonymous functions when named functions are clearer**
If an anonymous function is more than 3 lines, extract it to a named function.
Long anonymous functions are hard to read and cannot be individually tested.

**5. Creating closures in loops without understanding capture semantics**
Unlike JavaScript (pre-`let`), Elixir closures always capture the correct value
in each iteration because bindings are immutable. No loop-variable bug.

---

## Resources

- [Anonymous functions — Elixir Getting Started](https://elixir-lang.org/getting-started/basic-types.html#anonymous-functions)
- [Function — HexDocs](https://hexdocs.pm/elixir/Function.html)
- [Closures — Elixir School](https://elixirschool.com/en/lessons/basics/functions#anonymous-functions-1)
- [Enum — HexDocs](https://hexdocs.pm/elixir/Enum.html)
